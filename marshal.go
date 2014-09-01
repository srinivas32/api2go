package api2go

import (
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
)

type marshalingContext struct {
	root     map[string]interface{}
	rootName string
}

func makeContext(rootName string) *marshalingContext {
	ctx := &marshalingContext{}
	ctx.rootName = rootName
	ctx.root = map[string]interface{}{}
	ctx.root[rootName] = []interface{}{}
	return ctx
}

// Marshal takes a struct (or slice of structs) and marshals them to a json encodable interface{} value
func Marshal(data interface{}) (interface{}, error) {
	if data == nil {
		panic("nil passed to Marshal")
	}

	var ctx *marshalingContext

	if reflect.TypeOf(data).Kind() == reflect.Slice {
		// We were passed a slice
		// Using Elem() here to get the slice's element type
		rootName := pluralize(jsonify(reflect.TypeOf(data).Elem().Name()))

		// Panic if empty string, i.e. passed []interface{}
		if rootName == "" {
			panic("You passed a slice of interfaces []interface{}{...} to Marshal. We cannot determine key names from that. Use []YourObjectName{...} instead.")
		}
		ctx = makeContext(rootName)

		// Marshal all elements
		// We iterate using reflections to save copying the slice to a []interface{}
		sliceValue := reflect.ValueOf(data)
		for i := 0; i < sliceValue.Len(); i++ {
			if err := ctx.marshalStruct(sliceValue.Index(i)); err != nil {
				return nil, err
			}
		}
	} else {
		// We were passed a single object
		rootName := pluralize(jsonify(reflect.TypeOf(data).Name()))
		ctx = makeContext(rootName)

		// Marshal the value
		if err := ctx.marshalStruct(reflect.ValueOf(data)); err != nil {
			return nil, err
		}
	}

	return ctx.root, nil
}

// marshalStruct marshals a struct and places it in the context's root
func (ctx *marshalingContext) marshalStruct(val reflect.Value) error {
	result := map[string]interface{}{}
	linksMap := map[string]interface{}{}

	valType := val.Type()
	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		keyName := jsonify(valType.Field(i).Name)

		if field.Kind() == reflect.Slice {
			// A slice indicates nested objects.

			// First, check whether this is a slice of structs which we need to nest
			if field.Type().Elem().Kind() == reflect.Struct {
				ids := []interface{}{}
				for i := 0; i < field.Len(); i++ {
					if idVal := field.Index(i).FieldByName("ID"); idVal.IsValid() {
						idString, err := toID(idVal)
						if err != nil {
							return err
						}
						ids = append(ids, idString)
					} else {
						panic("structs passed to Marshal need to contain ID fields")
					}

					if err := ctx.marshalStruct(field.Index(i)); err != nil {
						return err
					}
				}
				linksMap[keyName] = ids
			} else {
				// Treat slices of non-struct type as lists of IDs
				keyName = strings.TrimSuffix(keyName, "IDs")
				linksMapReflect := reflect.TypeOf(linksMap[keyName])
				// Don't overwrite any existing links, since they came from nested structs
				if linksMap[keyName] == nil || linksMapReflect.Kind() == reflect.Slice && len(linksMap[keyName].([]interface{})) == 0 {
					ids := []interface{}{}
					for i := 0; i < field.Len(); i++ {
						id, err := toID(field.Index(i))
						if err != nil {
							return err
						}
						ids = append(ids, id)
					}
					linksMap[keyName] = ids
				}
			}
		} else if keyName == "id" {
			// ID needs to be converted to string
			id, err := toID(field)
			if err != nil {
				return err
			}
			result[keyName] = id
		} else if field.Type().Kind() == reflect.Struct {
			if value := field.FieldByName("ID"); value.IsValid() {
				id, err := toID(value)
				if err != nil {
					return err
				}
				if id != "0" {
					linksMap[keyName] = id
				}
			}
		} else {
			result[keyName] = field.Interface()
		}
	}

	if len(linksMap) > 0 {
		result["links"] = linksMap
	}

	ctx.addValue(pluralize(jsonify(valType.Name())), result)
	return nil
}

// addValue adds an object to the context's root
// `name` should be the pluralized and underscorized object type.
func (ctx *marshalingContext) addValue(name string, val map[string]interface{}) {
	if name == ctx.rootName {
		// Root objects are placed directly into the root doc
		// BUG(lucas): If an object links to its own type, linked objects must be placed into the linked map.
		ctx.root[name] = append(ctx.root[name].([]interface{}), val)
	} else {
		// Linked objects are placed in a map under the `linked` key
		var linkedMap map[string][]interface{}
		if ctx.root["linked"] == nil {
			linkedMap = map[string][]interface{}{}
			ctx.root["linked"] = linkedMap
		} else {
			linkedMap = ctx.root["linked"].(map[string][]interface{})
		}
		if s := linkedMap[name]; s != nil {
			// check if already in linked list
			alreadyLinked := false
			for _, linked := range s {
				m := reflect.ValueOf(linked).Interface().(map[string]interface{})
				if val["id"] == m["id"] {
					alreadyLinked = true
				}
			}
			if !alreadyLinked {
				linkedMap[name] = append(s, val)
			}
		} else {
			linkedMap[name] = []interface{}{val}
		}
	}
}

// toID converts a value to a ID string
func toID(v reflect.Value) (string, error) {
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10), nil
	case reflect.String:
		return v.String(), nil
	default:
		return "", errors.New("need int or string as type of ID")
	}
}

// MarshalToJSON takes a struct and marshals it to JSONAPI compliant JSON
func MarshalToJSON(val interface{}) ([]byte, error) {
	result, err := Marshal(val)
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}
