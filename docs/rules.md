# Built-in Rules

Maybe Don't comes with a set of built-in rules to help secure your AI tool calls.

## CEL Rules

### Security Rules

#### Block Destructive Actions

```yaml
rules:
  - name: "block-destructive-actions"
    description: "Block destructive kubectl commands"
    expression: |
      has(params.arguments.command) &&
      (
        get(params.arguments.command, "").contains("delete") ||
        get(params.arguments.command, "").contains("remove") ||
        get(params.arguments.command, "").contains("destroy")
      )
    action: "block"
    message: "Destructive actions are not allowed"
```

#### Block System File Modifications

```yaml
rules:
  - name: "block-system-files"
    description: "Block modifications to system files"
    expression: |
      has(params.arguments.path) &&
      (
        get(params.arguments.path, "").startsWith("/etc/") ||
        get(params.arguments.path, "").startsWith("/var/") ||
        get(params.arguments.path, "").startsWith("/usr/")
      )
    action: "block"
    message: "Modifying system files is not allowed"
```

#### Block Path Traversal

```yaml
rules:
  - name: "block-path-traversal"
    description: "Block path traversal attacks"
    expression: |
      has(params.arguments.path) &&
      (
        get(params.arguments.path, "").contains("..") ||
        get(params.arguments.path, "").contains("./") ||
        get(params.arguments.path, "").contains("/.")
      )
    action: "block"
    message: "Path traversal attacks are not allowed"
```

### Resource Rules

#### Block Resource Deletion

```yaml
rules:
  - name: "block-resource-deletion"
    description: "Block deletion of critical resources"
    expression: |
      has(params.arguments.resource) &&
      (
        get(params.arguments.resource, "").contains("namespace") ||
        get(params.arguments.resource, "").contains("cluster") ||
        get(params.arguments.resource, "").contains("node")
      )
    action: "block"
    message: "Deleting critical resources is not allowed"
```

#### Block Resource Modification

```yaml
rules:
  - name: "block-resource-modification"
    description: "Block modification of critical resources"
    expression: |
      has(params.arguments.resource) &&
      (
        get(params.arguments.resource, "").contains("config") ||
        get(params.arguments.resource, "").contains("secret") ||
        get(params.arguments.resource, "").contains("certificate")
      )
    action: "block"
    message: "Modifying critical resources is not allowed"
```

## AI Rules

### Security Rules

#### Block Destructive Actions

```yaml
policies:
  - name: "block-destructive-actions"
    description: "Block destructive kubectl commands"
    rules:
      - "Block any kubectl commands that delete or remove resources"
      - "Block any commands that modify system files"
      - "Block any commands that expose sensitive information"
```

#### Block Path Traversal

```yaml
policies:
  - name: "block-path-traversal"
    description: "Block path traversal attacks"
    rules:
      - "Block any file operations that use .. or . in paths"
      - "Block any attempts to access files outside the allowed directory"
      - "Block any attempts to create symbolic links"
```

### Resource Rules

#### Block Resource Deletion

```yaml
policies:
  - name: "block-resource-deletion"
    description: "Block deletion of critical resources"
    rules:
      - "Block any attempts to delete namespaces"
      - "Block any attempts to delete clusters"
      - "Block any attempts to delete nodes"
      - "Block any attempts to delete critical system resources"
```

#### Block Resource Modification

```yaml
policies:
  - name: "block-resource-modification"
    description: "Block modification of critical resources"
    rules:
      - "Block any attempts to modify configuration files"
      - "Block any attempts to modify secrets"
      - "Block any attempts to modify certificates"
      - "Block any attempts to modify critical system resources"
```

## Custom Rules

### Creating Custom CEL Rules

1. Create a new rule in `cel_rules.yaml`:

```yaml
rules:
  - name: "your-rule-name"
    description: "Description of your rule"
    expression: |
      # Your CEL expression here
      has(params.arguments.field) &&
      get(params.arguments.field, "").contains("value")
    action: "block"  # or "allow"
    message: "Your custom message"
```

2. Use custom functions:
   - `has(field)`: Check if a field exists
   - `get(field, default)`: Get a field's value with a default

### Creating Custom AI Rules

1. Create a new policy in `ai_rules.yaml`:

```yaml
policies:
  - name: "your-policy-name"
    description: "Description of your policy"
    rules:
      - "Your first rule in natural language"
      - "Your second rule in natural language"
      - "Your third rule in natural language"
```

2. Use clear, specific language
3. Include examples if helpful
4. Be explicit about what to block

## Best Practices

1. **Rule Organization**
   - Group related rules together
   - Use descriptive names
   - Include clear messages

2. **Rule Specificity**
   - Be specific about what to block
   - Include examples
   - Consider edge cases

3. **Rule Testing**
   - Test with various inputs
   - Verify edge cases
   - Check error handling

4. **Rule Maintenance**
   - Document rules
   - Review regularly
   - Update as needed 