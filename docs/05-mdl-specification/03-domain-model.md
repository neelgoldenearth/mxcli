# MDL Domain Model

This document describes how MDL represents Mendix domain model concepts: entities, attributes, associations, validation rules, indexes, and access rules.

## Table of Contents

1. [Entities](#entities)
2. [Attributes](#attributes)
3. [Validation Rules](#validation-rules)
4. [Indexes](#indexes)
5. [Associations](#associations)
6. [Generalization (Inheritance)](#generalization)
7. [Access Rules](#access-rules)
8. [Event Handlers](#event-handlers)

---

## Entities

### Entity Types

| Type | MDL Keyword | Description |
|------|-------------|-------------|
| Persistent | `PERSISTENT` | Stored in database, has table |
| Non-Persistent | `NON-PERSISTENT` | In-memory only, session-scoped |
| View | `VIEW` | Based on OQL query, read-only |
| External | `EXTERNAL` | From external data source (OData, etc.) |

### Entity Syntax

```sql
[/** <documentation> */]
[@Position(<x>, <y>)]
CREATE [OR MODIFY] <entity-type> ENTITY <Module>.<Name> (
  <attribute-definitions>
)
[<index-definitions>]
[;|/]
```

### Entity Properties

| Property | MDL Representation | Description |
|----------|-------------------|-------------|
| Name | `Module.EntityName` | Qualified name |
| Documentation | `/** ... */` | Documentation comment before CREATE |
| Position | `@Position(x, y)` | Location in domain model diagram |
| Persistable | Entity type keyword | Whether stored in database |

### Examples

```sql
/** Persistent entity with all features */
@Position(100, 200)
CREATE PERSISTENT ENTITY Sales.Customer (
  CustomerId: AutoNumber NOT NULL UNIQUE DEFAULT 1,
  Name: String(200) NOT NULL,
  Email: String(200) UNIQUE
)
INDEX (Name);
/

/** Non-persistent entity for filtering */
CREATE NON-PERSISTENT ENTITY Sales.CustomerFilter (
  SearchName: String(200),
  IncludeInactive: Boolean DEFAULT FALSE
);
/

/** View entity with OQL */
CREATE VIEW ENTITY Reports.CustomerStats (
  CustomerName: String,
  OrderCount: Integer
) AS
  SELECT c.Name, COUNT(o.Id)
  FROM Sales.Customer c
  JOIN Sales.Order o ON o.Customer = c
  GROUP BY c.Name;
/
```

---

## Attributes

### Attribute Syntax

```sql
[/** <documentation> */]
<name>: <type> [<constraints>] [DEFAULT <value>]
```

### Attribute Properties

| Property | MDL Representation | Description |
|----------|-------------------|-------------|
| Name | `AttributeName:` | Attribute identifier |
| Documentation | `/** ... */` | Doc comment before attribute |
| Type | See [Data Types](./02-data-types.md) | Attribute data type |
| Required | `NOT NULL` | Value cannot be empty |
| Unique | `UNIQUE` | Value must be unique |
| Default | `DEFAULT value` | Default value on create |

### Attribute Ordering

Attributes are defined in order, separated by commas. The last attribute has no trailing comma:

```sql
CREATE PERSISTENT ENTITY Module.Entity (
  FirstAttr: String(200),      -- comma after
  SecondAttr: Integer,         -- comma after
  LastAttr: Boolean            -- no comma
);
```

---

## Validation Rules

Validation rules are expressed as attribute constraints in MDL.

### Supported Validations

| Validation | MDL Syntax | Description |
|------------|------------|-------------|
| Required | `NOT NULL` | Attribute must have a value |
| Required with message | `NOT NULL ERROR 'message'` | Custom error message |
| Unique | `UNIQUE` | Value must be unique across all objects |
| Unique with message | `UNIQUE ERROR 'message'` | Custom error message |

### Validation Syntax

```sql
AttrName: Type NOT NULL [ERROR '<message>'] [UNIQUE [ERROR '<message>']]
```

### Examples

```sql
CREATE PERSISTENT ENTITY Sales.Product (
  -- Required only
  Name: String(200) NOT NULL,

  -- Required with custom error
  SKU: String(50) NOT NULL ERROR 'SKU is required for all products',

  -- Unique only
  Barcode: String(50) UNIQUE,

  -- Required and unique with custom errors
  ProductCode: String(20) NOT NULL ERROR 'Product code required'
                          UNIQUE ERROR 'Product code must be unique',

  -- Optional field (no validation)
  Description: String(unlimited)
);
```

### Validation Rule Mapping

| MDL | BSON RuleInfo.$Type | Description |
|-----|---------------------|-------------|
| `NOT NULL` | `DomainModels$RequiredRuleInfo` | Required validation |
| `UNIQUE` | `DomainModels$UniqueRuleInfo` | Uniqueness validation |
| (future) `RANGE(min, max)` | `DomainModels$RangeRuleInfo` | Range validation |
| (future) `REGEX(pattern)` | `DomainModels$RegexRuleInfo` | Pattern validation |

---

## Indexes

Indexes improve query performance for frequently searched attributes.

### Index Syntax

```sql
INDEX (<column> [ASC|DESC] [, <column> [ASC|DESC] ...])
```

### Index Properties

| Property | MDL Syntax | Description |
|----------|------------|-------------|
| Columns | `(col1, col2, ...)` | Indexed columns in order |
| Sort Order | `ASC` / `DESC` | Sort direction (default: ASC) |

### Examples

```sql
CREATE PERSISTENT ENTITY Sales.Order (
  OrderId: AutoNumber NOT NULL UNIQUE,
  OrderNumber: String(50) NOT NULL,
  CustomerId: Long,
  OrderDate: DateTime,
  Status: Enumeration(Sales.OrderStatus)
)
-- Single column index
INDEX (OrderNumber)

-- Composite index
INDEX (CustomerId, OrderDate DESC)

-- Multiple indexes
INDEX (Status);
/
```

### Index Guidelines

1. **Primary lookups** - Index columns used in WHERE clauses
2. **Foreign keys** - Index association attributes
3. **Sorting** - Index columns used in ORDER BY
4. **Composite order** - Put high-selectivity columns first

---

## Associations

Associations define relationships between entities.

### Association Types

| Type | MDL Keyword | Cardinality | Description |
|------|-------------|-------------|-------------|
| Reference | `Reference` | Many-to-One | Child references one parent |
| ReferenceSet | `ReferenceSet` | Many-to-Many | Both can have multiple |

### Association Syntax

```sql
[/** <documentation> */]
CREATE ASSOCIATION <Module>.<AssociationName>
  FROM <ParentEntity>
  TO <ChildEntity>
  TYPE <Reference|ReferenceSet>
  [OWNER <Default|Both|Parent|Child>]
  [DELETE_BEHAVIOR <behavior>]
[;|/]
```

### Association Properties

| Property | MDL Clause | Description |
|----------|------------|-------------|
| Name | `Module.Name` | Association identifier |
| Parent | `FROM Entity` | Parent (owner/many) side of relationship |
| Child | `TO Entity` | Child (referenced/one) side of relationship |
| Type | `TYPE Reference/ReferenceSet` | Cardinality type |
| Owner | `OWNER` | Which side can modify |
| Delete Behavior | `DELETE_BEHAVIOR` | What happens on delete |

### Owner Options

| Owner | Description |
|-------|-------------|
| `Default` | Child owns (can set/clear reference) |
| `Both` | Both sides can modify the association |
| `Parent` | Only parent can modify |
| `Child` | Only child can modify |

### Delete Behavior Options

| Behavior | MDL Keyword | Description |
|----------|-------------|-------------|
| Delete but keep references | `DELETE_BUT_KEEP_REFERENCES` | Delete object, nullify references |
| Delete cascade | `DELETE_CASCADE` | Delete associated objects too |

### Examples

```sql
/** Order belongs to Customer (many-to-one) */
CREATE ASSOCIATION Sales.Order_Customer
  FROM Sales.Customer
  TO Sales.Order
  TYPE Reference
  OWNER Default
  DELETE_BEHAVIOR DELETE_BUT_KEEP_REFERENCES;
/

/** Order has many Products (many-to-many) */
CREATE ASSOCIATION Sales.Order_Product
  FROM Sales.Order
  TO Sales.Product
  TYPE ReferenceSet
  OWNER Both;
/

/** Invoice must be deleted with Order */
CREATE ASSOCIATION Sales.Order_Invoice
  FROM Sales.Order
  TO Sales.Invoice
  TYPE Reference
  DELETE_BEHAVIOR DELETE_CASCADE;
/
```

---

## Generalization

Generalization (inheritance) allows entities to extend other entities.

### Generalization Syntax

```sql
CREATE PERSISTENT ENTITY <Module>.<Name>
  EXTENDS <ParentEntity>
(
  <additional-attributes>
);
```

Both `EXTENDS` (preferred) and `GENERALIZATION` (legacy) keywords are supported. The `EXTENDS` keyword can appear before the attribute list or as an entity option after it.

### System Generalizations

Common system entity generalizations:

| Parent Entity | Purpose |
|---------------|---------|
| `System.User` | User accounts |
| `System.FileDocument` | File storage |
| `System.Image` | Image storage |

### Examples

```sql
/** Employee extends User with additional fields */
CREATE PERSISTENT ENTITY HR.Employee EXTENDS System.User (
  EmployeeNumber: String(20) NOT NULL UNIQUE,
  Department: String(100),
  HireDate: Date
);

/** Image entity for product photos */
CREATE PERSISTENT ENTITY Catalog.ProductPhoto EXTENDS System.Image (
  Caption: String(200),
  SortOrder: Integer DEFAULT 0
);

/** File attachment entity */
CREATE PERSISTENT ENTITY Docs.Attachment EXTENDS System.FileDocument (
  Description: String(500)
);
```

---

## Access Rules

Access rules control entity-level security. They are managed via `GRANT` and `REVOKE` statements.

### Syntax

```sql
-- Grant entity access to a module role
GRANT <module>.<role> ON <module>.<entity> (<rights>) [WHERE '<xpath>'];

-- Revoke entity access
REVOKE <module>.<role> ON <module>.<entity>;

-- Show access on an entity
SHOW ACCESS ON <module>.<entity>;

-- Show security matrix
SHOW SECURITY MATRIX [IN <module>];
```

Where `<rights>` is a comma-separated list of:
- `CREATE` — allow creating instances
- `DELETE` — allow deleting instances
- `READ *` — read all members, or `READ (<attr>, ...)` for specific attributes
- `WRITE *` — write all members, or `WRITE (<attr>, ...)` for specific attributes

### Examples

```sql
-- Full access
GRANT Sales.Admin ON Sales.Customer (CREATE, DELETE, READ *, WRITE *);

-- Read-only
GRANT Sales.Viewer ON Sales.Customer (READ *);

-- Selective member access
GRANT Sales.User ON Sales.Customer (READ (Name, Email), WRITE (Email));

-- With XPath constraint
GRANT Sales.User ON Sales.Order (READ *, WRITE *) WHERE '[Status = ''Open'']';

-- Revoke
REVOKE Sales.User ON Sales.Order;
```

### Access Rule Properties

| Property | Description |
|----------|-------------|
| Role | Module role that rule applies to |
| Create | Can create new objects |
| Read | Can read objects (all or specific members) |
| Write | Can modify objects (all or specific members) |
| Delete | Can delete objects |
| XPath | Constraint on which objects (optional) |

---

## Event Handlers

Event handlers trigger microflows on entity lifecycle events.

**Note:** Event handlers are not yet expressible in MDL syntax.

### Planned Syntax

```sql
CREATE PERSISTENT ENTITY Sales.Order (
  ...
)
EVENTS (
  ON CREATE CALL Sales.Order_OnCreate,
  ON COMMIT CALL Sales.Order_Validate RAISE_ERROR,
  ON DELETE CALL Sales.Order_OnDelete
);
```

### Event Types

| Event | When Triggered |
|-------|----------------|
| Create | After object is created in memory |
| Commit | Before object is committed to database |
| Delete | Before object is deleted |
| Rollback | When transaction is rolled back |

---

## Complete Domain Model Example

```sql
-- Connect to project
CONNECT LOCAL './MyApp.mpr';

-- Create enumeration
CREATE ENUMERATION Sales.OrderStatus (
  Draft 'Draft',
  Pending 'Pending',
  Confirmed 'Confirmed',
  Shipped 'Shipped',
  Delivered 'Delivered',
  Cancelled 'Cancelled'
);

-- Create Customer entity
/** Customer master data */
@Position(100, 100)
CREATE PERSISTENT ENTITY Sales.Customer (
  CustomerId: AutoNumber NOT NULL UNIQUE DEFAULT 1,
  Name: String(200) NOT NULL ERROR 'Customer name is required',
  Email: String(200) UNIQUE ERROR 'Email already registered',
  Phone: String(50),
  IsActive: Boolean DEFAULT TRUE,
  CreatedAt: DateTime
)
INDEX (Name)
INDEX (Email);
/

-- Create Order entity
/** Sales order */
@Position(300, 100)
CREATE PERSISTENT ENTITY Sales.Order (
  OrderId: AutoNumber NOT NULL UNIQUE DEFAULT 1,
  OrderNumber: String(50) NOT NULL UNIQUE,
  OrderDate: DateTime NOT NULL,
  TotalAmount: Decimal DEFAULT 0,
  Status: Enumeration(Sales.OrderStatus) DEFAULT 'Draft',
  Notes: String(unlimited)
)
INDEX (OrderNumber)
INDEX (OrderDate DESC);
/

-- Create association
CREATE ASSOCIATION Sales.Order_Customer
  FROM Sales.Customer
  TO Sales.Order
  TYPE Reference
  OWNER Default
  DELETE_BEHAVIOR DELETE_BUT_KEEP_REFERENCES;
/

-- Show result
SHOW ENTITIES IN Sales;
DESCRIBE ENTITY Sales.Customer;
DESCRIBE ENTITY Sales.Order;
DESCRIBE ASSOCIATION Sales.Order_Customer;

COMMIT MESSAGE 'Created Sales domain model';
```
