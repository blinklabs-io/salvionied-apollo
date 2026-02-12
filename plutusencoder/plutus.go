package plutusencoder

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"
	"reflect"

	"github.com/blinklabs-io/plutigo/data"
)

// PlutusMarshaler is the interface for custom plutus data encoding/decoding.
type PlutusMarshaler interface {
	ToPlutusData() (data.PlutusData, error)
	FromPlutusData(pd data.PlutusData, res any) error
}

// MarshalPlutus encodes a Go struct to PlutusData using struct tags.
func MarshalPlutus(v any) (data.PlutusData, error) {
	return marshalValue(reflect.ValueOf(v))
}

func marshalValue(val reflect.Value) (data.PlutusData, error) {
	// Dereference pointers
	for val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return nil, errors.New("nil pointer")
		}
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return nil, fmt.Errorf("MarshalPlutus requires a struct, got %s", val.Kind())
	}

	// Check if the type implements PlutusMarshaler
	if val.CanAddr() {
		if m, ok := val.Addr().Interface().(PlutusMarshaler); ok {
			return m.ToPlutusData()
		}
	}

	typ := val.Type()

	// Read container tags from the anonymous `_` field
	containerType := ""
	constrTag := uint(0)
	hasConstr := false

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Name == "_" {
			containerType = field.Tag.Get("plutusType")
			if constrStr := field.Tag.Get("plutusConstr"); constrStr != "" {
				var c uint64
				_, err := fmt.Sscanf(constrStr, "%d", &c)
				if err == nil {
					constrTag = uint(c)
					hasConstr = true
				}
			}
			break
		}
	}

	switch containerType {
	case "Map":
		return marshalMap(val, typ, constrTag, hasConstr)
	default:
		// IndefList, DefList, or no tag (default to DefList)
		useIndef := containerType == "IndefList"
		return marshalList(val, typ, constrTag, hasConstr, useIndef)
	}
}

func marshalList(val reflect.Value, typ reflect.Type, constrTag uint, hasConstr bool, useIndef bool) (data.PlutusData, error) {
	var fields []data.PlutusData

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Name == "_" || !field.IsExported() {
			continue
		}

		fieldVal := val.Field(i)
		pd, err := marshalField(fieldVal, field)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", field.Name, err)
		}
		fields = append(fields, pd)
	}

	if hasConstr {
		return data.NewConstrDefIndef(useIndef, constrTag, fields...), nil
	}
	return data.NewListDefIndef(useIndef, fields...), nil
}

func marshalMap(val reflect.Value, typ reflect.Type, constrTag uint, hasConstr bool) (data.PlutusData, error) {
	var pairs [][2]data.PlutusData

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Name == "_" || !field.IsExported() {
			continue
		}

		fieldVal := val.Field(i)

		keyName := field.Tag.Get("plutusKey")
		if keyName == "" {
			keyName = field.Name
		}

		key := data.NewByteString([]byte(keyName))
		value, err := marshalField(fieldVal, field)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", field.Name, err)
		}
		pairs = append(pairs, [2]data.PlutusData{key, value})
	}

	if hasConstr {
		mapData := data.NewMap(pairs)
		return data.NewConstr(constrTag, mapData), nil
	}
	return data.NewMap(pairs), nil
}

func marshalField(fieldVal reflect.Value, field reflect.StructField) (data.PlutusData, error) {
	plutusType := field.Tag.Get("plutusType")

	// BigInt handles nil *big.Int directly, so dispatch before pointer dereference
	if plutusType == "BigInt" {
		return marshalBigInt(fieldVal)
	}

	// Dereference pointers
	for fieldVal.Kind() == reflect.Ptr {
		if fieldVal.IsNil() {
			return nil, fmt.Errorf("nil pointer for field %s", field.Name)
		}
		fieldVal = fieldVal.Elem()
	}

	// Check for PlutusMarshaler interface
	if fieldVal.CanAddr() {
		if m, ok := fieldVal.Addr().Interface().(PlutusMarshaler); ok {
			return m.ToPlutusData()
		}
	}

	switch plutusType {
	case "Int":
		return marshalInt(fieldVal)
	case "Bytes":
		return marshalBytes(fieldVal)
	case "StringBytes":
		return marshalStringBytes(fieldVal)
	case "HexString":
		return marshalHexString(fieldVal)
	case "Bool":
		return marshalBool(fieldVal, false)
	case "IndefBool":
		return marshalBool(fieldVal, true)
	case "IndefList":
		return marshalSliceOrNested(fieldVal, field, true)
	case "DefList":
		return marshalSliceOrNested(fieldVal, field, false)
	case "Map":
		return marshalSliceAsMap(fieldVal, field)
	case "Custom":
		if fieldVal.CanAddr() {
			if m, ok := fieldVal.Addr().Interface().(PlutusMarshaler); ok {
				return m.ToPlutusData()
			}
		}
		return nil, fmt.Errorf("field %s tagged Custom but doesn't implement PlutusMarshaler", field.Name)
	default:
		// No tag - recursively marshal as nested struct
		if fieldVal.Kind() == reflect.Struct {
			return marshalValue(fieldVal)
		}
		return nil, fmt.Errorf("unsupported field type %s for field %s", fieldVal.Kind(), field.Name)
	}
}

func marshalInt(val reflect.Value) (data.PlutusData, error) {
	switch val.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return data.NewInteger(big.NewInt(val.Int())), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return data.NewInteger(new(big.Int).SetUint64(val.Uint())), nil
	default:
		return nil, fmt.Errorf("int tag requires integer type, got %s", val.Kind())
	}
}

func marshalBigInt(val reflect.Value) (data.PlutusData, error) {
	switch v := val.Interface().(type) {
	case *big.Int:
		if v == nil {
			return data.NewInteger(big.NewInt(0)), nil
		}
		return data.NewInteger(v), nil
	case big.Int:
		return data.NewInteger(&v), nil
	default:
		return nil, fmt.Errorf("BigInt tag requires *big.Int or big.Int, got %T", val.Interface())
	}
}

func marshalBytes(val reflect.Value) (data.PlutusData, error) {
	if val.Kind() != reflect.Slice || val.Type().Elem().Kind() != reflect.Uint8 {
		return nil, fmt.Errorf("bytes tag requires []byte, got %s", val.Type())
	}
	return data.NewByteString(val.Bytes()), nil
}

func marshalStringBytes(val reflect.Value) (data.PlutusData, error) {
	if val.Kind() != reflect.String {
		return nil, fmt.Errorf("StringBytes tag requires string, got %s", val.Kind())
	}
	return data.NewByteString([]byte(val.String())), nil
}

func marshalHexString(val reflect.Value) (data.PlutusData, error) {
	if val.Kind() != reflect.String {
		return nil, fmt.Errorf("HexString tag requires string, got %s", val.Kind())
	}
	b, err := hex.DecodeString(val.String())
	if err != nil {
		return nil, fmt.Errorf("HexString: invalid hex: %w", err)
	}
	return data.NewByteString(b), nil
}

func marshalBool(val reflect.Value, useIndef bool) (data.PlutusData, error) {
	if val.Kind() != reflect.Bool {
		return nil, fmt.Errorf("bool tag requires bool, got %s", val.Kind())
	}
	tag := uint(0)
	if val.Bool() {
		tag = 1
	}
	return data.NewConstrDefIndef(useIndef, tag), nil
}

func marshalSliceOrNested(val reflect.Value, field reflect.StructField, useIndef bool) (data.PlutusData, error) {
	if val.Kind() == reflect.Slice {
		var items []data.PlutusData
		for i := 0; i < val.Len(); i++ {
			elem := val.Index(i)
			pd, err := marshalSliceElement(elem)
			if err != nil {
				return nil, fmt.Errorf("element %d: %w", i, err)
			}
			items = append(items, pd)
		}
		return data.NewListDefIndef(useIndef, items...), nil
	}
	// Nested struct
	return marshalValue(val)
}

// marshalSliceElement marshals a single slice element, handling both struct and primitive types.
func marshalSliceElement(elem reflect.Value) (data.PlutusData, error) {
	for elem.Kind() == reflect.Ptr {
		if elem.IsNil() {
			return nil, errors.New("nil pointer in slice")
		}
		elem = elem.Elem()
	}
	switch elem.Kind() {
	case reflect.Struct:
		return marshalValue(elem)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return data.NewInteger(big.NewInt(elem.Int())), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return data.NewInteger(new(big.Int).SetUint64(elem.Uint())), nil
	case reflect.String:
		return data.NewByteString([]byte(elem.String())), nil
	case reflect.Slice:
		if elem.Type().Elem().Kind() == reflect.Uint8 {
			return data.NewByteString(elem.Bytes()), nil
		}
		return nil, fmt.Errorf("unsupported slice element type: %s", elem.Type())
	default:
		return nil, fmt.Errorf("unsupported slice element kind: %s", elem.Kind())
	}
}

func marshalSliceAsMap(val reflect.Value, field reflect.StructField) (data.PlutusData, error) {
	if val.Kind() == reflect.Slice {
		var pairs [][2]data.PlutusData
		for i := 0; i < val.Len(); i++ {
			elem := val.Index(i)
			for elem.Kind() == reflect.Ptr {
				if elem.IsNil() {
					return nil, fmt.Errorf("nil pointer at element %d", i)
				}
				elem = elem.Elem()
			}
			// Extract key from first exported field of each element
			key, err := extractMapKey(elem, i)
			if err != nil {
				return nil, fmt.Errorf("element %d key: %w", i, err)
			}
			pd, err := marshalValue(elem)
			if err != nil {
				return nil, fmt.Errorf("element %d: %w", i, err)
			}
			pairs = append(pairs, [2]data.PlutusData{key, pd})
		}
		return data.NewMap(pairs), nil
	}
	return marshalValue(val)
}

// extractMapKey gets the map key from a slice element.
// For structs, uses the first exported field's string value as a ByteString key.
// For other types, falls back to the integer index.
func extractMapKey(elem reflect.Value, index int) (data.PlutusData, error) {
	if elem.Kind() == reflect.Struct {
		typ := elem.Type()
		for j := 0; j < typ.NumField(); j++ {
			f := typ.Field(j)
			if f.Name == "_" || !f.IsExported() {
				continue
			}
			fv := elem.Field(j)
			if fv.Kind() == reflect.String {
				return data.NewByteString([]byte(fv.String())), nil
			}
			// For non-string first fields, marshal it
			pd, err := marshalField(fv, f)
			if err != nil {
				return nil, err
			}
			return pd, nil
		}
	}
	return data.NewInteger(big.NewInt(int64(index))), nil
}

// UnmarshalPlutus decodes PlutusData into a Go struct using struct tags.
func UnmarshalPlutus(pd data.PlutusData, v any) error {
	val := reflect.ValueOf(v)
	if val.Kind() != reflect.Ptr || val.IsNil() {
		return errors.New("UnmarshalPlutus requires a non-nil pointer")
	}
	return unmarshalValue(pd, val.Elem())
}

func unmarshalValue(pd data.PlutusData, val reflect.Value) error {
	// Check for PlutusMarshaler
	if val.CanAddr() {
		if m, ok := val.Addr().Interface().(PlutusMarshaler); ok {
			return m.FromPlutusData(pd, val.Addr().Interface())
		}
	}

	if val.Kind() != reflect.Struct {
		return fmt.Errorf("unmarshal target must be a struct, got %s", val.Kind())
	}

	typ := val.Type()

	// Read container type
	containerType := ""
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Name == "_" {
			containerType = field.Tag.Get("plutusType")
			break
		}
	}

	switch containerType {
	case "Map":
		return unmarshalFromMap(pd, val, typ)
	default:
		return unmarshalFromList(pd, val, typ)
	}
}

func unmarshalFromList(pd data.PlutusData, val reflect.Value, typ reflect.Type) error {
	var fields []data.PlutusData

	switch v := pd.(type) {
	case *data.Constr:
		fields = v.Fields
	case *data.List:
		fields = v.Items
	default:
		return fmt.Errorf("expected Constr or List, got %T", pd)
	}

	fieldIdx := 0
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Name == "_" || !field.IsExported() {
			continue
		}
		if fieldIdx >= len(fields) {
			break
		}

		fieldVal := val.Field(i)
		if err := unmarshalField(fields[fieldIdx], fieldVal, field); err != nil {
			return fmt.Errorf("field %s: %w", field.Name, err)
		}
		fieldIdx++
	}
	return nil
}

func unmarshalFromMap(pd data.PlutusData, val reflect.Value, typ reflect.Type) error {
	mapData, ok := pd.(*data.Map)
	if !ok {
		// Could be a Constr wrapping a Map
		if constr, ok := pd.(*data.Constr); ok && len(constr.Fields) == 1 {
			mapData, ok = constr.Fields[0].(*data.Map)
			if !ok {
				return fmt.Errorf("expected Map in Constr, got %T", constr.Fields[0])
			}
		} else {
			return fmt.Errorf("expected Map, got %T", pd)
		}
	}

	// Build a lookup from key name to PlutusData
	keyMap := make(map[string]data.PlutusData)
	for _, pair := range mapData.Pairs {
		if bs, ok := pair[0].(*data.ByteString); ok {
			keyMap[string(bs.Inner)] = pair[1]
		}
	}

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Name == "_" || !field.IsExported() {
			continue
		}

		keyName := field.Tag.Get("plutusKey")
		if keyName == "" {
			keyName = field.Name
		}

		value, exists := keyMap[keyName]
		if !exists {
			continue
		}

		fieldVal := val.Field(i)
		if err := unmarshalField(value, fieldVal, field); err != nil {
			return fmt.Errorf("field %s: %w", field.Name, err)
		}
	}
	return nil
}

func unmarshalField(pd data.PlutusData, fieldVal reflect.Value, field reflect.StructField) error {
	plutusType := field.Tag.Get("plutusType")

	// BigInt handles *big.Int directly, so dispatch before pointer dereference
	if plutusType == "BigInt" {
		return unmarshalBigInt(pd, fieldVal)
	}

	// Dereference / allocate pointers
	for fieldVal.Kind() == reflect.Ptr {
		if fieldVal.IsNil() {
			fieldVal.Set(reflect.New(fieldVal.Type().Elem()))
		}
		fieldVal = fieldVal.Elem()
	}

	// Check for PlutusMarshaler
	if fieldVal.CanAddr() {
		if m, ok := fieldVal.Addr().Interface().(PlutusMarshaler); ok {
			return m.FromPlutusData(pd, fieldVal.Addr().Interface())
		}
	}

	switch plutusType {
	case "Int":
		return unmarshalInt(pd, fieldVal)
	case "Bytes":
		return unmarshalBytes(pd, fieldVal)
	case "StringBytes":
		return unmarshalStringBytes(pd, fieldVal)
	case "HexString":
		return unmarshalHexString(pd, fieldVal)
	case "Bool", "IndefBool":
		return unmarshalBool(pd, fieldVal)
	case "IndefList", "DefList":
		return unmarshalSliceOrNested(pd, fieldVal, field)
	case "Map":
		return unmarshalSliceAsMap(pd, fieldVal, field)
	default:
		// Nested struct
		if fieldVal.Kind() == reflect.Struct {
			return unmarshalValue(pd, fieldVal)
		}
		return fmt.Errorf("unsupported field type %s for field %s", fieldVal.Kind(), field.Name)
	}
}

func unmarshalInt(pd data.PlutusData, fieldVal reflect.Value) error {
	integer, ok := pd.(*data.Integer)
	if !ok {
		return fmt.Errorf("expected Integer, got %T", pd)
	}
	switch fieldVal.Kind() {
	case reflect.Int, reflect.Int64:
		if !integer.Inner.IsInt64() {
			return fmt.Errorf("integer value %s does not fit in int64", integer.Inner.String())
		}
		fieldVal.SetInt(integer.Inner.Int64())
	case reflect.Int32:
		if !integer.Inner.IsInt64() {
			return fmt.Errorf("integer value %s does not fit in int32", integer.Inner.String())
		}
		v := integer.Inner.Int64()
		if v < math.MinInt32 || v > math.MaxInt32 {
			return fmt.Errorf("integer value %d does not fit in int32", v)
		}
		fieldVal.SetInt(v)
	case reflect.Int16:
		if !integer.Inner.IsInt64() {
			return fmt.Errorf("integer value %s does not fit in int16", integer.Inner.String())
		}
		v := integer.Inner.Int64()
		if v < math.MinInt16 || v > math.MaxInt16 {
			return fmt.Errorf("integer value %d does not fit in int16", v)
		}
		fieldVal.SetInt(v)
	case reflect.Int8:
		if !integer.Inner.IsInt64() {
			return fmt.Errorf("integer value %s does not fit in int8", integer.Inner.String())
		}
		v := integer.Inner.Int64()
		if v < math.MinInt8 || v > math.MaxInt8 {
			return fmt.Errorf("integer value %d does not fit in int8", v)
		}
		fieldVal.SetInt(v)
	case reflect.Uint, reflect.Uint64:
		if integer.Inner.Sign() < 0 || !integer.Inner.IsUint64() {
			return fmt.Errorf("integer value %s does not fit in uint64", integer.Inner.String())
		}
		fieldVal.SetUint(integer.Inner.Uint64())
	case reflect.Uint32:
		if integer.Inner.Sign() < 0 || !integer.Inner.IsUint64() {
			return fmt.Errorf("integer value %s does not fit in uint32", integer.Inner.String())
		}
		v := integer.Inner.Uint64()
		if v > math.MaxUint32 {
			return fmt.Errorf("integer value %d does not fit in uint32", v)
		}
		fieldVal.SetUint(v)
	case reflect.Uint16:
		if integer.Inner.Sign() < 0 || !integer.Inner.IsUint64() {
			return fmt.Errorf("integer value %s does not fit in uint16", integer.Inner.String())
		}
		v := integer.Inner.Uint64()
		if v > math.MaxUint16 {
			return fmt.Errorf("integer value %d does not fit in uint16", v)
		}
		fieldVal.SetUint(v)
	case reflect.Uint8:
		if integer.Inner.Sign() < 0 || !integer.Inner.IsUint64() {
			return fmt.Errorf("integer value %s does not fit in uint8", integer.Inner.String())
		}
		v := integer.Inner.Uint64()
		if v > math.MaxUint8 {
			return fmt.Errorf("integer value %d does not fit in uint8", v)
		}
		fieldVal.SetUint(v)
	default:
		return fmt.Errorf("int tag requires integer type, got %s", fieldVal.Kind())
	}
	return nil
}

func unmarshalBigInt(pd data.PlutusData, fieldVal reflect.Value) error {
	integer, ok := pd.(*data.Integer)
	if !ok {
		return fmt.Errorf("expected Integer, got %T", pd)
	}
	switch fieldVal.Type() {
	case reflect.TypeOf((*big.Int)(nil)):
		fieldVal.Set(reflect.ValueOf(new(big.Int).Set(integer.Inner)))
	case reflect.TypeOf(big.Int{}):
		fieldVal.Set(reflect.ValueOf(*new(big.Int).Set(integer.Inner)))
	default:
		return fmt.Errorf("BigInt tag requires *big.Int or big.Int, got %s", fieldVal.Type())
	}
	return nil
}

func unmarshalBytes(pd data.PlutusData, fieldVal reflect.Value) error {
	bs, ok := pd.(*data.ByteString)
	if !ok {
		return fmt.Errorf("expected ByteString, got %T", pd)
	}
	if fieldVal.Kind() != reflect.Slice || fieldVal.Type().Elem().Kind() != reflect.Uint8 {
		return fmt.Errorf("bytes tag requires []byte, got %s", fieldVal.Type())
	}
	fieldVal.SetBytes(bs.Inner)
	return nil
}

func unmarshalStringBytes(pd data.PlutusData, fieldVal reflect.Value) error {
	bs, ok := pd.(*data.ByteString)
	if !ok {
		return fmt.Errorf("expected ByteString, got %T", pd)
	}
	if fieldVal.Kind() != reflect.String {
		return fmt.Errorf("StringBytes tag requires string, got %s", fieldVal.Kind())
	}
	fieldVal.SetString(string(bs.Inner))
	return nil
}

func unmarshalHexString(pd data.PlutusData, fieldVal reflect.Value) error {
	bs, ok := pd.(*data.ByteString)
	if !ok {
		return fmt.Errorf("expected ByteString, got %T", pd)
	}
	if fieldVal.Kind() != reflect.String {
		return fmt.Errorf("HexString tag requires string, got %s", fieldVal.Kind())
	}
	fieldVal.SetString(hex.EncodeToString(bs.Inner))
	return nil
}

func unmarshalBool(pd data.PlutusData, fieldVal reflect.Value) error {
	constr, ok := pd.(*data.Constr)
	if !ok {
		return fmt.Errorf("expected Constr for Bool, got %T", pd)
	}
	if fieldVal.Kind() != reflect.Bool {
		return fmt.Errorf("bool tag requires bool, got %s", fieldVal.Kind())
	}
	fieldVal.SetBool(constr.Tag == 1)
	return nil
}

func unmarshalSliceOrNested(pd data.PlutusData, fieldVal reflect.Value, field reflect.StructField) error {
	if fieldVal.Kind() == reflect.Slice {
		var items []data.PlutusData
		switch v := pd.(type) {
		case *data.List:
			items = v.Items
		case *data.Constr:
			items = v.Fields
		default:
			return fmt.Errorf("expected List or Constr for slice, got %T", pd)
		}

		elemType := fieldVal.Type().Elem()
		result := reflect.MakeSlice(fieldVal.Type(), len(items), len(items))
		for i, item := range items {
			elem := reflect.New(elemType).Elem()
			if err := unmarshalSliceElement(item, elem); err != nil {
				return fmt.Errorf("element %d: %w", i, err)
			}
			result.Index(i).Set(elem)
		}
		fieldVal.Set(result)
		return nil
	}
	// Nested struct
	return unmarshalValue(pd, fieldVal)
}

// unmarshalSliceElement unmarshals a single slice element, handling both struct and primitive types.
func unmarshalSliceElement(pd data.PlutusData, elem reflect.Value) error {
	switch elem.Kind() {
	case reflect.Struct:
		return unmarshalValue(pd, elem)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		integer, ok := pd.(*data.Integer)
		if !ok {
			return fmt.Errorf("expected Integer, got %T", pd)
		}
		if !integer.Inner.IsInt64() {
			return fmt.Errorf("integer value %s does not fit in %s", integer.Inner.String(), elem.Kind())
		}
		v := integer.Inner.Int64()
		switch elem.Kind() {
		case reflect.Int8:
			if v < math.MinInt8 || v > math.MaxInt8 {
				return fmt.Errorf("integer value %d does not fit in int8", v)
			}
		case reflect.Int16:
			if v < math.MinInt16 || v > math.MaxInt16 {
				return fmt.Errorf("integer value %d does not fit in int16", v)
			}
		case reflect.Int32:
			if v < math.MinInt32 || v > math.MaxInt32 {
				return fmt.Errorf("integer value %d does not fit in int32", v)
			}
		}
		elem.SetInt(v)
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		integer, ok := pd.(*data.Integer)
		if !ok {
			return fmt.Errorf("expected Integer, got %T", pd)
		}
		if integer.Inner.Sign() < 0 || !integer.Inner.IsUint64() {
			return fmt.Errorf("integer value %s does not fit in %s", integer.Inner.String(), elem.Kind())
		}
		v := integer.Inner.Uint64()
		switch elem.Kind() {
		case reflect.Uint8:
			if v > math.MaxUint8 {
				return fmt.Errorf("integer value %d does not fit in uint8", v)
			}
		case reflect.Uint16:
			if v > math.MaxUint16 {
				return fmt.Errorf("integer value %d does not fit in uint16", v)
			}
		case reflect.Uint32:
			if v > math.MaxUint32 {
				return fmt.Errorf("integer value %d does not fit in uint32", v)
			}
		}
		elem.SetUint(v)
		return nil
	case reflect.String:
		bs, ok := pd.(*data.ByteString)
		if !ok {
			return fmt.Errorf("expected ByteString, got %T", pd)
		}
		elem.SetString(string(bs.Inner))
		return nil
	case reflect.Slice:
		if elem.Type().Elem().Kind() == reflect.Uint8 {
			bs, ok := pd.(*data.ByteString)
			if !ok {
				return fmt.Errorf("expected ByteString, got %T", pd)
			}
			elem.SetBytes(bs.Inner)
			return nil
		}
		return fmt.Errorf("unsupported nested slice type: %s", elem.Type())
	default:
		return fmt.Errorf("unsupported slice element kind: %s", elem.Kind())
	}
}

func unmarshalSliceAsMap(pd data.PlutusData, fieldVal reflect.Value, field reflect.StructField) error {
	if fieldVal.Kind() == reflect.Slice {
		mapData, ok := pd.(*data.Map)
		if !ok {
			return fmt.Errorf("expected Map for slice, got %T", pd)
		}

		elemType := fieldVal.Type().Elem()
		result := reflect.MakeSlice(fieldVal.Type(), len(mapData.Pairs), len(mapData.Pairs))
		for i, pair := range mapData.Pairs {
			elem := reflect.New(elemType).Elem()
			if err := unmarshalValue(pair[1], elem); err != nil {
				return fmt.Errorf("element %d: %w", i, err)
			}
			result.Index(i).Set(elem)
		}
		fieldVal.Set(result)
		return nil
	}
	return unmarshalValue(pd, fieldVal)
}
