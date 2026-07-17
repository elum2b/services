package runtime

import (
	"fmt"
	"math"
	"time"

	lua "github.com/yuin/gopher-lua"
)

func goToLua(L *lua.LState, value any) lua.LValue {
	switch v := value.(type) {
	case nil:
		return lua.LNil
	case lua.LValue:
		return v
	case bool:
		return lua.LBool(v)
	case string:
		return lua.LString(v)
	case []byte:
		return lua.LString(v)
	case int:
		return lua.LNumber(v)
	case int32:
		return lua.LNumber(v)
	case int64:
		return lua.LNumber(v)
	case uint64:
		return lua.LNumber(v)
	case float32:
		return lua.LNumber(v)
	case float64:
		return lua.LNumber(v)
	case time.Time:
		if v.IsZero() {
			return lua.LNil
		}
		return lua.LString(v.UTC().Format(time.RFC3339Nano))
	case []any:
		table := L.NewTable()
		for _, item := range v {
			table.Append(goToLua(L, item))
		}
		return table
	case map[string]any:
		table := L.NewTable()
		for key, item := range v {
			table.RawSetString(key, goToLua(L, item))
		}
		return table
	default:
		return lua.LString(fmt.Sprint(v))
	}
}

func luaToGo(value lua.LValue) any {
	switch v := value.(type) {
	case *lua.LNilType:
		return nil
	case lua.LBool:
		return bool(v)
	case lua.LString:
		return string(v)
	case lua.LNumber:
		number := float64(v)
		if math.Trunc(number) == number && number >= math.MinInt64 && number <= math.MaxInt64 {
			return int64(number)
		}
		return number
	case *lua.LTable:
		if isLuaArray(v) {
			result := make([]any, 0, v.Len())
			for i := 1; i <= v.Len(); i++ {
				result = append(result, luaToGo(v.RawGetInt(i)))
			}
			return result
		}
		result := make(map[string]any)
		v.ForEach(func(key lua.LValue, val lua.LValue) {
			result[key.String()] = luaToGo(val)
		})
		return result
	default:
		return value.String()
	}
}

func isLuaArray(table *lua.LTable) bool {
	length := table.Len()
	array := true
	count := 0
	table.ForEach(func(key lua.LValue, _ lua.LValue) {
		count++
		number, ok := key.(lua.LNumber)
		if !ok || int(number) < 1 || int(number) > length || float64(number) != math.Trunc(float64(number)) {
			array = false
		}
	})
	return array && count == length
}
