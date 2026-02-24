# Building an AI Database Assistant with SingleStore and GenAI Toolbox


## What You'll Build

By the end of this tutorial, you'll have an AI assistant that can:

- Explore database schemas through conversation
- Generate and execute SQL queries from natural language
- Answer business questions without writing SQL
- Explain query results in plain English
- Handle follow-up questions with full context

## Prerequisites

- **SingleStore Instance**: [Sign up for free tier](https://www.singlestore.com/cloud-trial/) or use existing instance
- **GenAI Toolbox**: We'll install this in the setup steps
- **MCP Client**: Claude Desktop, Cursor, or any MCP-compatible IDE
- **Sample Database**: We'll create an e-commerce demo database

## Part 1: Set Up SingleStore Database

First, let's create a sample e-commerce database to work with.

### 1.1 Connect to SingleStore

```bash
# If using SingleStore Cloud, get connection details from portal
# Example connection:
mysql -h <your-host> -P <port> -u <username> -p'<password>' <database-name>
```

### 1.2 Create Sample Database

Copy and run this SQL to create our e-commerce schema:

```sql
-- Customers table
CREATE TABLE customers (
    customer_id INT AUTO_INCREMENT PRIMARY KEY,
    customer_name VARCHAR(100) NOT NULL,
    email VARCHAR(100) NOT NULL,
    state VARCHAR(2),
    city VARCHAR(100),
    signup_date DATE,
    INDEX idx_state (state)
);

-- Products table
CREATE TABLE products (
    product_id INT AUTO_INCREMENT PRIMARY KEY,
    product_name VARCHAR(200) NOT NULL,
    category VARCHAR(50),
    price DECIMAL(10, 2),
    stock_quantity INT DEFAULT 0,
    INDEX idx_category (category)
);

-- Orders table
CREATE TABLE orders (
    order_id INT AUTO_INCREMENT PRIMARY KEY,
    customer_id INT NOT NULL,
    order_date DATETIME DEFAULT NOW(),
    total_amount DECIMAL(10, 2),
    status VARCHAR(20) DEFAULT 'pending',
    FOREIGN KEY (customer_id) REFERENCES customers(customer_id),
    INDEX idx_order_date (order_date),
    INDEX idx_customer (customer_id)
);

-- Order items table
CREATE TABLE order_items (
    order_item_id INT AUTO_INCREMENT PRIMARY KEY,
    order_id INT NOT NULL,
    product_id INT NOT NULL,
    quantity INT NOT NULL,
    price DECIMAL(10, 2) NOT NULL,
    INDEX idx_order (order_id)
);

-- Insert sample data
INSERT INTO customers (customer_id, customer_name, email, state, city, signup_date) VALUES
    (1, 'Alice Johnson', 'alice@example.com', 'CA', 'San Francisco', '2024-01-15'),
    (2, 'Bob Smith', 'bob@example.com', 'NY', 'New York', '2024-02-20'),
    (3, 'Carol White', 'carol@example.com', 'CA', 'Los Angeles', '2024-03-10'),
    (4, 'David Brown', 'david@example.com', 'TX', 'Austin', '2024-01-25'),
    (5, 'Eve Davis', 'eve@example.com', 'WA', 'Seattle', '2024-04-05');

INSERT INTO products (product_id, product_name, category, price, stock_quantity) VALUES
    (1, 'Laptop Pro 15', 'Electronics', 1299.99, 50),
    (2, 'Wireless Mouse', 'Electronics', 29.99, 200),
    (3, 'Office Chair', 'Furniture', 299.99, 75),
    (4, 'Desk Lamp', 'Furniture', 49.99, 150),
    (5, 'USB-C Cable', 'Accessories', 12.99, 500);

INSERT INTO orders (order_id, customer_id, order_date, total_amount, status) VALUES
    (1, 1, '2024-10-15 10:30:00', 1329.98, 'completed'),
    (2, 2, '2024-11-20 14:15:00', 349.98, 'completed'),
    (3, 3, '2024-12-05 09:45:00', 62.97, 'shipped'),
    (4, 1, '2024-12-18 16:20:00', 299.99, 'pending'),
    (5, 4, '2024-11-10 11:00:00', 1312.98, 'completed');

INSERT INTO order_items (order_id, product_id, quantity, price) VALUES
    (1, 1, 1, 1299.99),
    (1, 2, 1, 29.99),
    (2, 3, 1, 299.99),
    (2, 4, 1, 49.99),
    (2, 4, 1, 49.99),
    (3, 2, 2, 29.99),
    (3, 5, 1, 12.99),
    (4, 3, 1, 299.99),
    (5, 1, 1, 1299.99),
    (5, 5, 1, 12.99);
```

Your database is now ready! Let's verify:

```sql
-- Quick verification
SELECT 'Customers' as table_name, COUNT(*) as row_count FROM customers
UNION ALL
SELECT 'Products', COUNT(*) FROM products
UNION ALL
SELECT 'Orders', COUNT(*) FROM orders
UNION ALL
SELECT 'Order Items', COUNT(*) FROM order_items;
```

You should see 5 customers, 5 products, 5 orders, and 9 order items.

## Part 2: Install and Configure GenAI Toolbox

TODO: rename GenAI to MCP
### 2.1 Install GenAI Toolbox

```bash
# Download the latest release, visit https://github.com/googleapis/genai-toolbox/releases
# if the links below don't work or your OS is not listed here

export VERSION=0.27.0
# For macOS (Apple Silicon):
curl -L -o genai-toolbox https://storage.googleapis.com/genai-toolbox/v$VERSION/darwin/arm64/toolbox

# For macOS (Intel):
curl -L -o genai-toolbox https://storage.googleapis.com/genai-toolbox/v$VERSION/darwin/amd64/toolbox

# For Linux (genai-toolbox):
curl -L -o toolbox https://storage.googleapis.com/genai-toolbox/v$VERSION/linux/amd64/toolbox

chmod +x genai-toolbox
# Move to your  /usr/local/bin/ or other place in PATH
sudo mv genai-toolbox /usr/local/bin/

# Verify installation
genai-toolbox --version
```

### 2.2 Create SingleStore Configuration

Create a configuration file that specifies connection parameters for your SingleStore database. You can define custom tools available to MCP clients here as well.

Create `singlestore-config.yaml`:

```yaml
# singlestore-config.yaml
sources:
  my-singlestore:
    kind: singlestore
    host: ${SINGLESTORE_HOST}
    port: ${SINGLESTORE_PORT}
    database: ${SINGLESTORE_DATABASE}
    user: ${SINGLESTORE_USER}
    password: ${SINGLESTORE_PASSWORD}
    queryTimeout: 30s
```

### 2.3 Set Environment Variables

Create `.singlestore.env` file with your connection credentials. Replace the values with your actual SingleStore credentials.

```bash
# .singlestore.env
SINGLESTORE_HOST=<your-host>
SINGLESTORE_PORT=<yout-port, usually 3306>
SINGLESTORE_DATABASE=<your-database>
SINGLESTORE_USER=<your-username>
SINGLESTORE_PASSWORD=<your-password>
```

Secure the file:
```bash
chmod 600 .singlestore.env
```

### 2.4 Test the Configuration

```bash
# Load environment variables
export $(cat .singlestore.env | xargs)

# Start GenAI Toolbox with MCP
genai-toolbox --tools-file singlestore-config.yaml
```

You should see output indicating the MCP server is running and tools are registered. You can stop the process with Ctrl+C after checking that `genai-toolbox` can be started with your config.

## Part 3: Connect Your MCP Client

Now let's connect an AI client to use these tools. We'll use Claude CLI as an example, but the process is similar for Cursor, Cline, or other MCP-compatible clients.

### 3.1 Configure Claude Desktop

Edit the configuration file `.mcp.json` by adding SingleStore MCP server:

```json
{
  "mcpServers": {
    "singlestore-demo": {
      "command": "genai-toolbox",
      "args": [
        "--prebuilt", "singlestore",
        "--tools", "singlestore-config.yaml",
        "--stdio"
      ],
      "env": {
        "SINGLESTORE_HOST":"<your-host>",
        "SINGLESTORE_PORT":"<yout-port>",
        "SINGLESTORE_DATABASE":"<your-database>",
        "SINGLESTORE_USER":"<your-username>",
        "SINGLESTORE_PASSWORD":"<your-password>"
      }
    }
  }
}
```

### 3.2 Restart Claude Desktop

After saving the configuration to `.mcp.json`:

1. Quit Claude CLI completely
2. Reopen it
3. Run `/mcp`. You should see "connected" next to `singlestore-demo`


### 3.3 Verify Connection

In Claude CLI, send this message:

> "Can you list the available tools for SingleStore?"

Claude should respond with information about the `execute_sql` and `list_tables` tools.

## Part 4: Usage Examples

Now let's see how MCP + AI transforms database work.

### 1: Schema Exploration

**Traditional SQL Editor:**
```sql
SHOW TABLES;
DESCRIBE customers;
DESCRIBE orders;
DESCRIBE order_items;
DESCRIBE products;
-- You have to manually piece together the relationships
```

**With MCP + AI:**

Simply ask:
> "What tables exist in this database and how are they related?"

The AI will:
1. Call `list_tables` to get complete schema information
2. Analyze the foreign keys and relationships
3. Provide a clear explanation like:

*"Your ecommerce_demo database has 4 tables:*

*- **customers**: Stores customer information (name, email, location)*
*- **products**: Product catalog with pricing and inventory*
*- **orders**: Order records linked to customers via customer_id*
*- **order_items**: Individual items in each order, linking orders to products*

*The relationships form a classic e-commerce schema: customers place orders, and each order contains multiple order_items referencing products."*

---

### 2: Business Questions Without SQL

Let's say we want to answer the question "Which states generated the most revenue in the last 3 months of 2024?".
With a traditional SQL Editor, you'd have to figure out this query yourself:

```sql
SELECT
    c.state,
    COUNT(DISTINCT o.order_id) as num_orders,
    SUM(o.total_amount) as total_revenue,
    AVG(o.total_amount) as avg_order_value
FROM customers c
JOIN orders o ON c.customer_id = o.customer_id
WHERE o.order_date >= '2024-10-01'
AND o.order_date < '2025-01-01'
GROUP BY c.state
ORDER BY total_revenue DESC;
```

**With MCP + AI:**

Ask naturally:
> "Which states generated the most revenue in the last 3 months of 2024?"

The AI:
1. Understands you need to join `customers` and `orders`.
2. Generates the  query and executes it.
3. Formats the results with insights.

*"Here are the top states by revenue in Q4 2024:*
*1. CA (California): $1,692.95 from 2 orders*
*2. TX (Texas): $1,312.98 from 1 order*
*3. NY (New York): $349.98 from 1 order"*

**Follow-up questions work seamlessly:**

> "Show me the customers from California"

> "What did the Texas customer order?"

> "Compare this to the previous quarter"

Each follow-up is answered instantly without rewriting queries.


## Part 5: Extensions
Let's look at a few ways to expand the functionality of our integration.

### Custom Tools for Repeated Tasks

You can create custom tools for frequently-used queries. For example, add to your config:

```yaml
# singlestore-config.yaml
tools:
  top_customers:
    kind: singlestore-sql
    source: my-singlestore
    description: Get top N customers by lifetime value
    statement: |
      SELECT
          c.customer_name,
          c.email,
          SUM(o.total_amount) as lifetime_value,
          COUNT(o.order_id) as order_count
      FROM customers c
      JOIN orders o ON c.customer_id = o.customer_id
      WHERE o.status = 'completed'
      GROUP BY c.customer_id, c.customer_name, c.email
      ORDER BY lifetime_value DESC
      LIMIT ?
    parameters:
      - name: limit
        type: integer
        description: Number of top customers to return
        default: 10
```

Now you can ask:
> "Show me the top 5 customers"

The AI will use your custom `top_customers` tool automatically.


### Learn More

- [GenAI Toolbox Documentation](https://github.com/googleapis/genai-toolbox)
- [SingleStore Documentation](https://docs.singlestore.com/)
- [MCP Protocol Specification](https://modelcontextprotocol.io/)
- [More Source Integrations](https://github.com/googleapis/genai-toolbox/tree/main/docs/en/resources/sources)
