/*
 * Copyright (c) 2026 Tranquil Data, Inc. All rights reserved.
 */

package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// FlatJSONProvider returns a static Provider for a very simple config file encoded in JSON.
// It supports only a "flat" structure with no sub-objects and arrays that only contain base
// types. Each key is interpreted as a property name, and each value the value for that
// property. If the structure does not meet these requirements, if the file cannot be read
// and unmarshalled, or if any key is invalid against the given scheme an error is returned.
// Once a valid Provider is returned it is "static", and will not change even if the
// underlying file is modified.
//
// Here is an example of a valid file for the CAMEL naming scheme:
//
//	{
//	    "httpsPort": 8443,
//	    "requireClientAuth": true,
//	    "allowedSignatureAlgs": ["sha256WithRSA", "sha256WithECDSA", "sha3_256WithRSA"]
//	}
func FlatJSONProvider(filename string, scheme NamingScheme) (Provider, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("error reading config file: %s", err.Error())
	}

	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()

	var jsonMap map[string]any
	err = decoder.Decode(&jsonMap)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling config file: %s", err.Error())
	} else if _, err := decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("error unmarshalling config file: unexpected content after JSON object")
	}

	propertyMap := make(map[string]string, len(jsonMap))
	for k, v := range jsonMap {
		if err = scheme.Validate(k); err != nil {
			return nil, fmt.Errorf("invalid property name in configuration file: %s", err.Error())
		}
		if propertyMap[k], err = jsonToPropertyValue(v); err != nil {
			return nil, fmt.Errorf("invalid value for property %s in configuration file: %s", k, err.Error())
		}
	}

	return MapProvider(propertyMap), nil
}

func jsonToPropertyValue(jsonValue any) (string, error) {
	switch typedValue := jsonValue.(type) {

	case nil:
		return "", fmt.Errorf("null values are not supported")

	case map[string]any:
		return "", fmt.Errorf("objects are not supported")

	case []any:
		// arrays are encoded the same way as slice values from any other provider, so
		// each element must be a scalar that doesn't itself contain the separator
		elements := make([]string, len(typedValue))
		for i, element := range typedValue {
			switch element.(type) {
			case nil:
				return "", fmt.Errorf("arrays may not contain null values")
			case map[string]any, []any:
				return "", fmt.Errorf("arrays may only contain scalar values")
			}
			elements[i] = fmt.Sprintf("%v", element)
			if strings.Contains(elements[i], SliceValueSeparator) {
				return "", fmt.Errorf("array element contains the slice separator: %s", elements[i])
			}
		}
		return strings.Join(elements, SliceValueSeparator), nil

	default:
		return fmt.Sprintf("%v", jsonValue), nil
	}
}
