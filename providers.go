/*
 * Copyright (c) 2026 Tranquil Data, Inc. All rights reserved.
 */

package config

import (
	"fmt"
	"maps"
	"os"
	"regexp"
	"strings"
)

// KVSeparator is the separator used for name-value formatted strings.
const KVSeparator = "="

// EnvironmentNameSeparator is the separator used for environment variable names.
const EnvironmentNameSeparator = "_"

// NoEnvironmentPrefix is used to skip prefixing environment variable names.
const NoEnvironmentPrefix = ""

// environmentPrefixExpression matches on a valid environment variable name prefix.
var environmentPrefixExpression = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*$`)

// staticProvider provides only the values from a known map.
type staticProvider struct {
	properties map[string]string
}

// environmentProvider provides values from the environment.
type environmentProvider struct {
	prefix string
	scheme NamingScheme
}

// chainedProvider provides values from one provider, fall-back on another provider
// if the first provider in the chain did not provide a value.
type chainedProvider struct {
	thisProvider, nextProvider Provider
}

// ListProvider returns a static Provider that will provide all properties from kvElements.
// The values in kvElement are name-value pairs of the form "NAME=VALUE", and it is an error
// for the list to contain duplicate names. It is also an error if any name is not valid
// against the given scheme. The value should be any valid base type, or slice of base type,
// that the Service will be asked to load.
func ListProvider(kvElements []string, scheme NamingScheme) (Provider, error) {
	properties := map[string]string{}
	for _, element := range kvElements {
		if components := strings.Split(strings.TrimSpace(element), KVSeparator); len(components) == 2 {
			if err := scheme.Validate(components[0]); err != nil {
				return nil, err
			}
			if _, present := properties[components[0]]; present {
				return nil, fmt.Errorf("property provided more than once: %s", components[0])
			}
			properties[components[0]] = components[1]
		} else {
			return nil, fmt.Errorf("misformed property element: %s", element)
		}
	}

	return &staticProvider{
		properties: properties,
	}, nil
}

// MapProvider returns a static Provider that treats the keys in the map as property names.
// No scheme is provided, so this can be used for testing or custom application needs.
func MapProvider(properties map[string]string) Provider {
	return &staticProvider{
		properties: maps.Clone(properties),
	}
}

// EnvironmentProvider returns a Provider that looks up property values from the environment.
// The value of prefix is pre-pended to each environment variable name so that callers can
// easily group and discover their variables. A prefix may only contain letters, numbers, and
// underscores, and an error is returned if the prefix is not valid. To skip using a prefix,
// use NoEnvironmentPrefix.
//
// The provided scheme is used to form the name of an environment variable for a given
// property. For instance, if the scheme is CAMEL, then the name "somePropertyName" becomes
// "SOME_PROPERTY_NAME". If the prefix is "app" then the full environment variable name becomes
// "APP_SOME_PROPERTY_NAME".
//
// Note that this Provider's KnownProperties() always returns an empty map. In the case where
// a prefix is provided it could attempt to return all variables with that prefix, but does not
// always return the right answer. This is still an open area where feedback would be welcome.
func EnvironmentProvider(prefix string, scheme NamingScheme) (Provider, error) {
	// the prefix starts every variable name, so it must be something that can be set from a shell
	prefix = strings.TrimSpace(prefix)
	if prefix != NoEnvironmentPrefix && !environmentPrefixExpression.MatchString(prefix) {
		return nil, fmt.Errorf("environment prefix must start with a letter and contain only letters, digits and underscores: %s", prefix)
	}

	return &environmentProvider{
		prefix: strings.ToUpper(prefix),
		scheme: scheme,
	}, nil
}

// ChainedProvider returns a Provider that will attempt to resolve values from
// thisProvider. If no value is returned (the bool return is false) then the Provider
// will query nextProvider. This is useful as a way to define an order of preference
// to search for values from different sources. This provider's KnownProperties are
// the sum of its two Provider's known properties.
func ChainedProvider(thisProvider, nextProvider Provider) Provider {
	return &chainedProvider{
		thisProvider: thisProvider,
		nextProvider: nextProvider,
	}
}

/* Implement Provider for staticProvider */

func (sp *staticProvider) Value(propertyName string) (string, bool) {
	value, present := sp.properties[propertyName]
	return value, present
}

func (sp *staticProvider) KnownProperties() map[string]string {
	return maps.Clone(sp.properties)
}

/* Implement Provider for environmentProvider */

func (ep *environmentProvider) Value(propertyName string) (string, bool) {
	envName := ep.prefix
	for _, component := range ep.scheme.Components(propertyName) {
		if envName != "" {
			envName += EnvironmentNameSeparator
		}
		envName += strings.ToUpper(component)
	}
	return os.LookupEnv(envName)
}

func (ep *environmentProvider) KnownProperties() map[string]string {
	// TODO: decide if this should return an empty set or all variables
	return map[string]string{}
}

/* Implement Provider for chainedProvider */

func (cp *chainedProvider) Value(propertyName string) (string, bool) {
	if value, present := cp.thisProvider.Value(propertyName); present {
		return value, true
	}
	return cp.nextProvider.Value(propertyName)
}

func (cp *chainedProvider) KnownProperties() map[string]string {
	// merge into a new map, rather than either provider's result, with this provider copied
	// last so that its values take priority the same way they do in Value
	properties := map[string]string{}
	maps.Copy(properties, cp.nextProvider.KnownProperties())
	maps.Copy(properties, cp.thisProvider.KnownProperties())
	return properties
}
