# Policy Engine

The policy engine is the core component of Maybe Don't that evaluates tool calls against defined rules.

## Overview

The policy engine consists of two main components:

1. **CEL Engine**: Evaluates rules using Google's Common Expression Language
2. **AI Engine**: Evaluates rules using OpenAI's GPT models

## CEL Engine

The CEL engine provides deterministic rule evaluation using Google's Common Expression Language.

### Features

- Parallel rule evaluation
- Custom functions for safe field access
- Deterministic results
- High performance

### Custom Functions

The CEL engine provides two custom functions:

1. `has(field)`: Safely checks if a field exists
   ```cel
   has(params.arguments.command)
   ```

2. `get(field, default)`: Safely gets a field's value with a default
   ```cel
   get(params.arguments.command, "")
   ```

### Example Rules

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

## AI Engine

The AI engine provides contextual analysis of tool calls using OpenAI's GPT models.

### Features

- Natural language rules
- Contextual analysis
- Parallel evaluation
- Configurable models

### Example Rules

```yaml
policies:
  - name: "block-destructive-actions"
    description: "Block destructive kubectl commands"
    rules:
      - "Block any kubectl commands that delete or remove resources"
      - "Block any commands that modify system files"
      - "Block any commands that expose sensitive information"

  - name: "block-path-traversal"
    description: "Block path traversal attacks"
    rules:
      - "Block any file operations that use .. or . in paths"
      - "Block any attempts to access files outside the allowed directory"
      - "Block any attempts to create symbolic links"
```

## Policy Evaluation

### Process

1. **Request Parsing**
   - Tool call parsed into structured format
   - Fields extracted for evaluation

2. **Rule Loading**
   - CEL rules loaded from file
   - AI rules loaded from file
   - Rules compiled for evaluation

3. **Parallel Evaluation**
   - CEL rules evaluated in parallel
   - AI rules evaluated in parallel
   - Results collected

4. **Result Aggregation**
   - Results combined
   - Final decision made
   - Response generated

### Response Format

```json
{
  "valid": false,
  "message": "Destructive actions are not allowed",
  "policies": [
    {
      "name": "block-destructive-actions",
      "valid": false,
      "message": "Destructive actions are not allowed"
    }
  ]
}
```

## Configuration

### CEL Engine

```yaml
policy_validation:
  enabled: true
  rules_file: cel_rules.yaml
```

### AI Engine

```yaml
ai_validation:
  enabled: true
  endpoint: https://api.openai.com/v1
  model: gpt-4-turbo-preview
  rules_file: ai_rules.yaml
  api_key: ${OPENAI_API_KEY}
```

## Best Practices

1. **Rule Organization**
   - Group related rules together
   - Use descriptive names
   - Include clear messages

2. **Performance**
   - Keep rules simple
   - Use custom functions
   - Avoid complex expressions

3. **Security**
   - Validate all inputs
   - Use safe field access
   - Handle errors gracefully

4. **Maintenance**
   - Document rules
   - Test changes
   - Review regularly 