/*
 * Copyright (c) 2026 Tranquil Data, Inc. All rights reserved.
 */

package config

//go:generate go tool mockgen -destination=./mocks/mock_definition_types.go -package=mocks github.com/tranquildata/config NamingScheme,Provider,Postprocessor

// NamingScheme defines which property names are valid, and how a valid name is split into
// the words that make it up. The words are used to form names in other styles, such as the
// upper-case, underscore-separated names used for environment variables.
type NamingScheme interface {
	// Components splits a property name into the words that make it up, keeping the case of
	// each word as it appears in the name. For instance, the CAMEL scheme splits the name
	// "maxHTTPConnections" into ["max", "HTTP", "Connections"]. The name is not validated, so
	// callers should use Validate() first if the name has not already been checked.
	Components(propertyName string) []string

	// Compose joins words into a single property name in the style of this scheme. It is the
	// inverse of Components(), so composing the components of a valid name returns that name,
	// and it is used to form names from parts, such as prepending a prefix to a name. For
	// instance, the CAMEL scheme composes ["index", "postgres", "Port"] into the name
	// "indexPostgresPort". The result is not validated, so callers should use Validate() if
	// the words did not all come from valid names.
	Compose(components []string) string

	// Validate returns an error if the property name is not valid for this scheme.
	Validate(propertyName string) error
}

// Provider is a source of property values, such as a list of command-line arguments, the
// environment, or a config file. Values are always provided as strings, in the same form that
// can be given in a tag's DEFAULTVALUE, so a slice value is a semicolon-separated list. See
// PopulateStruct() for details. Providers may be combined with ChainedProvider() to define an
// order of preference between sources.
//
// A Provider may be used concurrently once it has been created, so implementations must be
// safe for concurrent use.
type Provider interface {
	// Value returns the value of the given property, and whether the property was found. Note
	// that an empty value is still a value, so a property that was provided with an empty
	// value returns "" and true.
	Value(propertyName string) (string, bool)

	// KnownProperties returns all of the properties that this provider is able to list, as a
	// map from property name to value. A new map is returned on each call, so it may be
	// modified by the caller. Note that some providers can't list everything they are able to
	// provide (see EnvironmentProvider()), so a property may be missing from this map even
	// though Value() would return it.
	KnownProperties() map[string]string
}

// Postprocessor may be implemented by any config struct that needs to do more work once it has
// been populated, such as validating combinations of values or deriving new values. Process()
// is called after all of the struct's fields have been populated, including any nested structs,
// and a nested struct that implements Postprocessor is processed before the struct containing
// it. Note that PopulateStruct() is given a pointer to the struct, so this interface is usually
// implemented with a pointer receiver.
type Postprocessor interface {
	// Process completes the populated struct. If an error is returned it becomes the error
	// returned when the struct was loaded.
	Process() error
}

// Service is an entry-point for an application's configuration. It represents a known collection
// of Naming Scheme and Provider working together in a verified environment. Developers will
// typically use the provided implementation of this interface, or define their own verion with
// custom behavior, instead of working with the other interfaces type in an ad hoc fashion.
type Service interface {
	// RootDir returns the absolute path of the application's root directory.
	RootDir() string

	// ResourceDir returns the directory that holds the application's resources, typically
	// including any config file.
	ResourceDir() string

	// LoadConfig populates the given config struct, which must be a pointer to a struct, using
	// the rules described by PopulateStruct(). Each property that is resolved, whether from a
	// provided value or from a default, is recorded so that it can be reported by
	// AllProperties(), HiddenProperties() and VisibleProperties(). If an error is returned then
	// no properties are recorded, but the struct will be partially populated by any fields
	// that were successfully populated before the error.
	LoadConfig(configStruct any) error

	// LoadProperty returns the string encoding of a single property's value, and whether the
	// property was found. This routine only has the name of a property, so it has no type
	// information to check against and no default to provide if the value is missing. Any
	// value that is returned will be considered to be a visible property. If the name is invalid
	// against the Service's NamingScheme an error is returned. This routine is provided to help
	// with testing and quick one-off tasks, or when property names are resolved at runtime and
	// cannot be included in struct tags, but callers are strongly encouraged to prefer using
	// LoadConfig() whenever possible.
	LoadProperty(propertyName string) (string, bool, error)

	// AllProperties returns every property resolved by LoadConfig() or LoadProperty(), both
	// visible and hidden, as a map from property name to string value. A new map is returned on
	// each call, so it may be modified by the caller.
	AllProperties() map[string]string

	// HiddenProperties returns only the properties resolved by LoadConfig() for fields tagged
	// with "hiddenconfig". These are typically values that shouldn't be shown to a user, such as
	// debugging flags, sensitive values, or inputs to features still being developed. A new map
	// is returned on each call, so it may be modified by the caller.
	HiddenProperties() map[string]string

	// VisibleProperties returns the properties resolved by LoadConfig() for fields tagged with
	// "config", and any properties resolved by LoadProperty(), which are safe to show to a user.
	// A new map is returned on each call, so it may be modified by the caller.
	VisibleProperties() map[string]string

	// KnownProperties returns all of the properties that the Service's providers are able to
	// list, whether or not they have been loaded, with the same order of preference used when
	// loading. See Provider for details. Note that this may include properties that are marked
	// in struct tags as hidden.
	KnownProperties() map[string]string
}
