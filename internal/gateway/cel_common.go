package gateway

import (
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
	"github.com/mark3labs/mcp-go/mcp"
)

// celToolVariable is the CEL variable that exposes an MCP tool call in a simpler
// shape than request.params. The public policy docs write rules against it
// (e.g. tool.name == "github__delete_file"), so every CEL environment that
// evaluates MCP tool-call expressions must declare it.
const celToolVariable = "tool"

// celMapHelperFunctions returns the custom has(map, key) and get(map, key, default)
// functions shared by all CEL environments. They give rules safe field access on
// dynamic maps such as tool arguments without erroring on missing keys.
func celMapHelperFunctions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Function("has",
			cel.Overload("has_map_string", []*cel.Type{cel.MapType(cel.StringType, cel.DynType), cel.StringType}, cel.BoolType,
				cel.BinaryBinding(func(lhs, rhs ref.Val) ref.Val {
					obj, ok := lhs.(traits.Mapper)
					if !ok {
						return types.Bool(false)
					}
					field, ok := rhs.(types.String)
					if !ok {
						return types.Bool(false)
					}
					_, found := obj.Find(field)
					return types.Bool(found)
				}),
			),
		),
		cel.Function("get",
			cel.Overload("get_map_string_dyn", []*cel.Type{cel.MapType(cel.StringType, cel.DynType), cel.StringType, cel.DynType}, cel.DynType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					if len(args) != 3 {
						return types.NewErr("get() requires exactly 3 arguments")
					}
					obj, ok := args[0].(traits.Mapper)
					if !ok {
						return args[2]
					}
					field, ok := args[1].(types.String)
					if !ok {
						return args[2]
					}
					val, found := obj.Find(field)
					if !found {
						return args[2]
					}
					return val
				}),
			),
		),
	}
}

// buildToolVariable builds the value of the CEL "tool" variable for an MCP tool call.
// name is the prefixed tool name as sent by the caller (same as request.params.name)
// and arguments mirrors request.params.arguments. Missing arguments become an empty
// map so has()/get() on tool.arguments evaluate instead of failing on null.
func buildToolVariable(req mcp.CallToolRequest) map[string]any {
	arguments := req.Params.Arguments
	switch args := req.Params.Arguments.(type) {
	case nil:
		arguments = map[string]any{}
	case map[string]any:
		if args == nil {
			arguments = map[string]any{}
		}
	}

	return map[string]any{
		"name":      req.Params.Name,
		"arguments": arguments,
	}
}
