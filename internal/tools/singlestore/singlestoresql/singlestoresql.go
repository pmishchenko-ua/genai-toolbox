// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package singlestoresql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	yaml "github.com/goccy/go-yaml"
	"github.com/googleapis/genai-toolbox/internal/sources"
	"github.com/googleapis/genai-toolbox/internal/sources/singlestore"
	"github.com/googleapis/genai-toolbox/internal/tools"
)

const kind string = "singlestore-sql"

func init() {
	if !tools.Register(kind, newConfig) {
		panic(fmt.Sprintf("tool kind %q already registered", kind))
	}
}

func newConfig(ctx context.Context, name string, decoder *yaml.Decoder) (tools.ToolConfig, error) {
	actual := Config{Name: name}
	if err := decoder.DecodeContext(ctx, &actual); err != nil {
		return nil, err
	}
	return actual, nil
}

type compatibleSource interface {
	SingleStorePool() *sql.DB
}

// validate compatible sources are still compatible
var _ compatibleSource = &singlestore.Source{}

var compatibleSources = [...]string{singlestore.SourceKind}

// Config defines the configuration for a SingleStore SQL tool.
type Config struct {
	Name               string           `yaml:"name" validate:"required"`
	Kind               string           `yaml:"kind" validate:"required"`
	Source             string           `yaml:"source" validate:"required"`
	Description        string           `yaml:"description" validate:"required"`
	Statement          string           `yaml:"statement" validate:"required"`
	AuthRequired       []string         `yaml:"authRequired"`
	Parameters         tools.Parameters `yaml:"parameters"`
	TemplateParameters tools.Parameters `yaml:"templateParameters"`
}

// validate interface
var _ tools.ToolConfig = Config{}

// ToolConfigKind returns the kind of the tool configuration.
func (cfg Config) ToolConfigKind() string {
	return kind
}

// Initialize sets up and returns a new Tool instance based on the provided configuration and available sources.
// It verifies that the specified source exists and is compatible, processes tool parameters, and constructs
// the necessary manifests for tool operation. Returns an initialized Tool or an error if setup fails.
//
// Parameters:
//   srcs - a map of available sources, keyed by source name.
//
// Returns:
//   tools.Tool - the initialized tool instance.
//   error      - an error if the source is missing, incompatible, or setup fails.
func (cfg Config) Initialize(srcs map[string]sources.Source) (tools.Tool, error) {
	// verify source exists
	rawS, ok := srcs[cfg.Source]
	if !ok {
		return nil, fmt.Errorf("no source named %q configured", cfg.Source)
	}

	// verify the source is compatible
	s, ok := rawS.(compatibleSource)
	if !ok {
		return nil, fmt.Errorf("invalid source for %q tool: source kind must be one of %q", kind, compatibleSources)
	}

	allParameters, paramManifest, paramMcpManifest, err := tools.ProcessParameters(cfg.TemplateParameters, cfg.Parameters)

	if err != nil {
		return nil, err
	}

	mcpManifest := tools.McpManifest{
		Name:        cfg.Name,
		Description: cfg.Description,
		InputSchema: paramMcpManifest,
	}

	// finish tool setup
	t := Tool{
		Name:               cfg.Name,
		Kind:               kind,
		Parameters:         cfg.Parameters,
		TemplateParameters: cfg.TemplateParameters,
		AllParams:          allParameters,
		Statement:          cfg.Statement,
		AuthRequired:       cfg.AuthRequired,
		Pool:               s.SingleStorePool(),
		manifest:           tools.Manifest{Description: cfg.Description, Parameters: paramManifest, AuthRequired: cfg.AuthRequired},
		mcpManifest:        mcpManifest,
	}
	return t, nil
}

// validate interface
var _ tools.Tool = Tool{}

// Tool represents a SingleStore SQL tool instance with its configuration, parameters, and database connection.
type Tool struct {
	Name               string           `yaml:"name"`
	Kind               string           `yaml:"kind"`
	AuthRequired       []string         `yaml:"authRequired"`
	Parameters         tools.Parameters `yaml:"parameters"`
	TemplateParameters tools.Parameters `yaml:"templateParameters"`
	AllParams          tools.Parameters `yaml:"allParams"`

	Pool        *sql.DB
	Statement   string
	manifest    tools.Manifest
	mcpManifest tools.McpManifest
}

// Invoke executes the SQL statement defined in the Tool using the provided context and parameter values.
// It resolves template parameters and standard parameters, executes the query, and processes the result rows.
// Each row is returned as a map with column names as keys and their corresponding values, handling special
// cases for JSON and string types. Returns a slice of maps representing the result set, or an error if any
// step fails.
//
// Parameters:
//   ctx    - The context for controlling cancellation and timeouts.
//   params - The parameter values to be used for the SQL statement.
//
// Returns:
//   - A slice of maps, where each map represents a row with column names as keys.
//   - An error if template resolution, parameter extraction, query execution, or result processing fails.
func (t Tool) Invoke(ctx context.Context, params tools.ParamValues, accessToken tools.AccessToken) (any, error) {
	paramsMap := params.AsMap()
	newStatement, err := tools.ResolveTemplateParams(t.TemplateParameters, t.Statement, paramsMap)
	if err != nil {
		return nil, fmt.Errorf("unable to extract template params %w", err)
	}

	newParams, err := tools.GetParams(t.Parameters, paramsMap)
	if err != nil {
		return nil, fmt.Errorf("unable to extract standard params %w", err)
	}

	sliceParams := newParams.AsSlice()
	results, err := t.Pool.QueryContext(ctx, newStatement, sliceParams...)
	if err != nil {
		return nil, fmt.Errorf("unable to execute query: %w", err)
	}

	cols, err := results.Columns()
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve rows column name: %w", err)
	}

	// create an array of values for each column, which can be re-used to scan each row
	rawValues := make([]any, len(cols))
	values := make([]any, len(cols))
	for i := range rawValues {
		values[i] = &rawValues[i]
	}
	defer results.Close()

	colTypes, err := results.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("unable to get column types: %w", err)
	}

	var out []any
	for results.Next() {
		err := results.Scan(values...)
		if err != nil {
			return nil, fmt.Errorf("unable to parse row: %w", err)
		}
		vMap := make(map[string]any)
		for i, name := range cols {
			val := rawValues[i]
			if val == nil {
				vMap[name] = nil
				continue
			}

			// mysql driver return []uint8 type for "TEXT", "VARCHAR", and "NVARCHAR"
			// we'll need to cast it back to string
			// TODO(SingleStore): check for other data types
			switch colTypes[i].DatabaseTypeName() {
			case "JSON":
				// unmarshal JSON data before storing to prevent double marshaling
				var unmarshaledData any
				err := json.Unmarshal(val.([]byte), &unmarshaledData)
				if err != nil {
					return nil, fmt.Errorf("unable to unmarshal json data %s", val)
				}
				vMap[name] = unmarshaledData
			case "TEXT", "VARCHAR", "NVARCHAR":
				vMap[name] = string(val.([]byte))
			default:
				vMap[name] = val
			}
		}
		out = append(out, vMap)
	}

	if err := results.Err(); err != nil {
		return nil, fmt.Errorf("errors encountered during row iteration: %w", err)
	}

	return out, nil
}

func (t Tool) ParseParams(data map[string]any, claims map[string]map[string]any) (tools.ParamValues, error) {
	return tools.ParseParams(t.AllParams, data, claims)
}

func (t Tool) Manifest() tools.Manifest {
	return t.manifest
}

func (t Tool) McpManifest() tools.McpManifest {
	return t.mcpManifest
}

func (t Tool) Authorized(verifiedAuthServices []string) bool {
	return tools.IsAuthorized(t.AuthRequired, verifiedAuthServices)
}

func (t Tool) RequiresClientAuthorization() bool {
	return false
}
