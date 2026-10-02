/*
 * Copyright (c) 2026 Tranquil Data, Inc. All rights reserved.
 */

package config

import (
	"fmt"
	"maps"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"
)

const (
	PublicTagName       = "config"
	PrivateTagName      = "hiddenconfig"
	TagValueSeparator   = ","
	SliceValueSeparator = ";"
)

// struct types that we handle as "base types"
var timeReflectType = reflect.TypeFor[time.Time]()
var urlReflectType = reflect.TypeFor[url.URL]()
var baseStructTypes = map[reflect.Type]bool{
	timeReflectType: true,
	urlReflectType:  true,
}

// other types that need to be differentiated
var durationReflectType = reflect.TypeFor[time.Duration]()

// PopulateStruct takes any struct type, and based on the given provider and scheme,
// attempts to fill in all field-values. The value of configStruct must be a pointer
// to a struct, not the value itself, or the call will fail. If a field is a base type
// and is not tagged with either "config" or "hiddenconfig" it will be ignored. If a
// field is tagged but is un-exported an error is returned. If a field is an exported
// struct or pointer to a struct then that struct will be recursively populated. Struct
// fields do not need to be tagged.
//
// The syntax for a tag is `TYPE:"PROPERTYNAME,DEFAULTVALUE"`. The TYPE is either "config"
// or "hiddenconfig", and the return from this function includes a map from property name to
// populated value first for the visible base type fields and then for the hidden fields.
// The PROPERTYNAME is any property name that is valid by the rules of the given scheme. The
// optional DEFAULTVALUE is a string that is used if the provider does not support the
// property, and must be a valid representation of the field type.
//
// For all base types, the field may be a slice. In that case, the value of DEFAULTVALUE, and
// any value that the provider returns, will be split on a semicolon-separated list. That is,
// if the value of DEFAULTVALUE is "1;2" then the slice field will have two elements, with
// values 1 and 2.
func PopulateStruct(configStruct any, provider Provider, scheme NamingScheme) (map[string]string, map[string]string, error) {
	visibleProperties := map[string]string{}
	hiddenProperties := map[string]string{}

	// make sure the input is a pointer to a struct that we can resolve, otherwise we
	// won't be able to resolve fields and assign property values
	structValue := reflect.ValueOf(configStruct)
	if structValue.Kind() != reflect.Pointer {
		return nil, nil, fmt.Errorf("input is not a pointer")
	}
	structValue = structValue.Elem()
	if structValue.Kind() != reflect.Struct {
		return nil, nil, fmt.Errorf("input did not point to a struct")
	}

	// get the type for the struct itself
	structType := structValue.Type()

	// iterate through each of the struct's fields
	for fieldIndex := 0; fieldIndex < structType.NumField(); fieldIndex++ {
		// for the indexed position get the struct field type information and the value
		// of the struct's field
		fieldType := structType.Field(fieldIndex)
		fieldValue := structValue.Field(fieldIndex)

		// attempt to handle the field, stopping if any error occurred
		if visible, hidden, err := handleField(fieldType, fieldValue, provider, scheme); err != nil {
			return nil, nil, err
		} else {
			maps.Copy(visibleProperties, visible)
			maps.Copy(hiddenProperties, hidden)
		}
	}

	// the struct has been fully filled-out, so if the struct implements the
	// Postprocessor interface it can be called now
	if processor, ok := configStruct.(Postprocessor); ok {
		return visibleProperties, hiddenProperties, processor.Process()
	}

	return visibleProperties, hiddenProperties, nil
}

// handleField takes a single field from a struct and handles both the recursive case where the
// field is a struct and the base-case where the field is a base type. It ensures that the
// property name is valid, and fills in the value based on the provider or the default value if
// the provider doesn't know the property. It returns maps (in order) for the visible and the
// hidden fields that were handled, or an error if the field could not be handled for any reason.
func handleField(fieldType reflect.StructField, fieldValue reflect.Value, provider Provider, scheme NamingScheme) (map[string]string, map[string]string, error) {
	// unexported fields can't be assigned through reflection, so they're skipped entirely,
	// which lets config structs hold private state like a mutex or third-party types, but
	// a tagged unexported field is almost certainly a mistake so it's reported
	if !fieldType.IsExported() {
		for _, tagName := range []string{PublicTagName, PrivateTagName} {
			if strings.TrimSpace(fieldType.Tag.Get(tagName)) != "" {
				return nil, nil, fmt.Errorf("field %s has a %s tag but is not exported", fieldType.Name, tagName)
			}
		}
		return map[string]string{}, map[string]string{}, nil
	}

	kind := fieldType.Type.Kind()

	// pointers are only supported as a way to reference a struct, so a pointer to any base
	// type (include the struct "base types" that we handle natively) is rejected
	if kind == reflect.Pointer {
		if elemType := fieldType.Type.Elem(); elemType.Kind() != reflect.Struct || baseStructTypes[elemType] {
			return nil, nil, fmt.Errorf("pointers to base types are not supported for field %s: %s", fieldType.Name, fieldType.Type.String())
		}
	}

	// if this field is a struct, or a pointer to a struct, then handle that structure
	// recursively and continue on to the next field, but only if the struct isn't
	// a "base type" like Time or URL which we handle directly
	if (kind == reflect.Pointer || kind == reflect.Struct) && !baseStructTypes[fieldType.Type] {
		// if this is struct then we just need the address, but if it's a pointer then it hasn't
		// been allocated so we need to create the struct first
		var embeddedStruct any
		if kind == reflect.Struct {
			embeddedStruct = fieldValue.Addr().Interface()
		} else {
			newFieldValue := reflect.New(fieldType.Type.Elem())
			fieldValue.Set(newFieldValue)
			embeddedStruct = newFieldValue.Interface()
		}

		// recurse with the struct, and then skip to the next field
		if visible, hidden, err := PopulateStruct(embeddedStruct, provider, scheme); err != nil {
			return nil, nil, err
		} else {
			return visible, hidden, nil
		}
	}

	// get the configuration tag, skipping any fields that aren't tagged, and rejecting any
	// fields that are ambiguously tagged as both visible and hidden
	publicTagValue := strings.TrimSpace(fieldType.Tag.Get(PublicTagName))
	privateTagValue := strings.TrimSpace(fieldType.Tag.Get(PrivateTagName))
	if publicTagValue != "" && privateTagValue != "" {
		return nil, nil, fmt.Errorf("field %s cannot have both %s and %s tags", fieldType.Name, PublicTagName, PrivateTagName)
	}
	visible := privateTagValue == ""
	tagValue := publicTagValue
	if !visible {
		tagValue = privateTagValue
	} else if tagValue == "" {
		return map[string]string{}, map[string]string{}, nil
	}

	// make sure the tag has a valid set of inputs
	values := strings.Split(tagValue, TagValueSeparator)
	if len(values) > 2 {
		return nil, nil, fmt.Errorf("too many tag inputs for field: %s", fieldType.Name)
	}

	// check that the property name is valid
	propertyName := values[0]
	if err := scheme.Validate(propertyName); err != nil {
		return nil, nil, err
	}

	// resolvedProperty will hold the map for the single property name & value, and is
	// left empty if there's neither a default nor a provided value
	resolvedProperty := map[string]string{}

	// if there's a default value, be sure it's valid
	var defaultValue string
	var value reflect.Value
	var err error
	if len(values) == 2 {
		defaultValue = values[1]
		if value, err = fieldToValue(defaultValue, fieldType.Type); err != nil {
			return nil, nil, fieldValueError("default", fieldType, visible, err)
		}
	}

	// get the provided value, which overrides any default value
	if providedValue, present := provider.Value(propertyName); present {
		if value, err = fieldToValue(providedValue, fieldType.Type); err != nil {
			return nil, nil, fieldValueError("provided", fieldType, visible, err)
		}
		resolvedProperty = map[string]string{propertyName: providedValue}
	} else if len(values) == 2 {
		resolvedProperty = map[string]string{propertyName: defaultValue}
	}

	// if there was no default value, and no value was available from the provider, then
	// we can either decide to return an error or leave the field un-assigned .. for now
	// we'll do the latter (in-part to support empty slices) but we may want some way to
	// communicate this case, like another function on the postprocessor so the caller
	// can decide what to do
	if !value.IsValid() {
		return map[string]string{}, map[string]string{}, nil
	}

	// finally, fill-in the value for the input struct
	fieldValue.Set(value)
	if visible {
		return resolvedProperty, map[string]string{}, nil
	} else {
		return map[string]string{}, resolvedProperty, nil
	}
}

func fieldValueError(source string, fieldType reflect.StructField, visible bool, err error) error {
	// parsing errors typically include the value being parsed, so for hidden fields only
	// the expected type is reported to avoid exposing the value in logs or output
	if visible {
		return fmt.Errorf("illegal %s value for field %s: %s", source, fieldType.Name, err.Error())
	}
	return fmt.Errorf("illegal %s value for hidden field %s: value is not a valid %s", source, fieldType.Name, fieldType.Type.String())
}

// fieldToValue resolves the value of a base type field, returning an error if the
// string representation is not valid for the type or if the base type is not known.
func fieldToValue(fieldValue string, fieldType reflect.Type) (reflect.Value, error) {
	var value reflect.Value
	emptyValue := reflect.Zero(fieldType)

	switch fieldType.Kind() {

	case reflect.String:
		value = reflect.ValueOf(fieldValue)

	case reflect.Bool:
		if parsedValue, err := strconv.ParseBool(fieldValue); err != nil {
			return emptyValue, err
		} else {
			value = reflect.ValueOf(parsedValue)
		}

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if fieldType == durationReflectType {
			if parsedValue, err := time.ParseDuration(fieldValue); err != nil {
				return emptyValue, err
			} else {
				value = reflect.ValueOf(parsedValue)
			}
		} else {
			if parsedValue, err := strconv.ParseInt(fieldValue, 10, fieldType.Bits()); err != nil {
				return emptyValue, err
			} else {
				value = reflect.ValueOf(parsedValue)
			}
		}

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if parsedValue, err := strconv.ParseUint(fieldValue, 10, fieldType.Bits()); err != nil {
			return emptyValue, err
		} else {
			value = reflect.ValueOf(parsedValue)
		}

	case reflect.Float32, reflect.Float64:
		if parsedValue, err := strconv.ParseFloat(fieldValue, fieldType.Bits()); err != nil {
			return emptyValue, err
		} else {
			value = reflect.ValueOf(parsedValue)
		}

	case reflect.Slice:
		if len(fieldValue) == 0 {
			value = reflect.MakeSlice(fieldType, 0, 0)
		} else {
			elements := strings.Split(fieldValue, SliceValueSeparator)
			value = reflect.MakeSlice(fieldType, len(elements), len(elements))
			for i, element := range elements {
				if elementValue, err := fieldToValue(strings.TrimSpace(element), fieldType.Elem()); err != nil {
					return emptyValue, err
				} else {
					value.Index(i).Set(elementValue)
				}
			}
		}

	case reflect.Struct:
		switch fieldType {
		case timeReflectType:
			if parsedValue, err := time.Parse(time.RFC3339, fieldValue); err != nil {
				return emptyValue, err
			} else {
				value = reflect.ValueOf(parsedValue)
			}

		case urlReflectType:
			if parsedValue, err := url.Parse(fieldValue); err != nil {
				return emptyValue, err
			} else {
				// url.Parse returns a pointer, but the field holds the url.URL value itself
				value = reflect.ValueOf(*parsedValue)
			}

		default:
			return emptyValue, fmt.Errorf("unsupported struct type in config structure: %s", fieldType.String())
		}

	default:
		return emptyValue, fmt.Errorf("unsupported type in config structure: %s", fieldType.String())
	}

	if value.Type() != fieldType {
		if !value.Type().ConvertibleTo(fieldType) {
			return emptyValue, fmt.Errorf("cannot convert %s to %s", value.Type().Name(), fieldType.Name())
		}
		value = value.Convert(fieldType)
	}

	return value, nil
}
