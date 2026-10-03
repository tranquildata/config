/*
 * Copyright (c) 2026 Tranquil Data, Inc. All rights reserved.
 */

package config

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// These are regexps that define and validate each of the supported naming schemes.
const (
	CamelCaseExpression = `^[a-z][a-zA-Z0-9]*$`
	SnakeCaseExpression = `^[a-z][a-z0-9]*(_[a-z0-9]+)*$`
	KebabCaseExpression = `^[a-zA-Z][a-zA-Z0-9]*(-[a-zA-Z0-9]+)*$`
)

type SplitFunction func(string) []string

type ComposeFunction func([]string) string

// namingScheme is a private utility that is used for all of the naming schemes.
type namingScheme struct {
	compiled        *regexp.Regexp
	splitFunction   SplitFunction
	composeFunction ComposeFunction
}

// CamelNamingScheme returns a NamingScheme that validates and enforces the CAMEL style
// of naming: "eachWordLikeThis". When a name is split by this NamingScheme, runs of
// upper-case characters are kept together. For instance, the name "maxHTTPConnections"
// would become ["max", "HTTP", "Connections"]. When names are composed, the first word is
// kept as-is and each following word starts with an upper-case character. For instance,
// ["index", "postgres", "Port"] would become "indexPostgresPort".
func CamelNamingScheme() NamingScheme {
	return &namingScheme{
		compiled:        regexp.MustCompile(CamelCaseExpression),
		splitFunction:   camelSplit,
		composeFunction: camelCompose,
	}
}

// SnakeNamingScheme returns a NamingScheme that validates and enforces the SNAKE style
// of naming: "each_word_like_this". When a name is split by this NamingScheme, all
// underscores are dropped. For instance, the name "cache_size" would become ["cache",
// "size"]. When names are composed, the words are joined with underscores.
func SnakeNamingScheme() NamingScheme {
	return &namingScheme{
		compiled:        regexp.MustCompile(SnakeCaseExpression),
		splitFunction:   snakeSplit,
		composeFunction: snakeCompose,
	}
}

// KebabNamingScheme returns a NamingScheme that validates and enforces the KEBAB style
// of naming: "each-word-like-this". When a name is split by this NamingScheme, all
// dashes are dropped. For instance, the name "cache-size" would become ["cache",
// "size"]. When names are composed, the words are joined with dashes.
func KebabNamingScheme() NamingScheme {
	return &namingScheme{
		compiled:        regexp.MustCompile(KebabCaseExpression),
		splitFunction:   kebabSplit,
		composeFunction: kebabCompose,
	}
}

/* Implement NamingScheme */

func (ns *namingScheme) Components(propertyName string) []string {
	return ns.splitFunction(propertyName)
}

func (ns *namingScheme) Compose(components []string) string {
	return ns.composeFunction(components)
}

func (ns *namingScheme) Validate(propertyName string) error {
	if !ns.compiled.MatchString(propertyName) {
		return fmt.Errorf("property name is invalid: %s", propertyName)
	}
	return nil
}

/* Implement static split functions */

func camelSplit(propertyName string) []string {
	components := []string{}
	if len(propertyName) == 0 {
		return components
	}

	// a new word starts at an upper-case letter that follows a non-upper-case character, or
	// at the last upper-case letter in a run when it's followed by a lower-case letter, so
	// that a run of capitals like "HTTP" in "maxHTTPPort" is kept together as one word
	runes := []rune(propertyName)
	start := 0
	for i := 1; i < len(runes); i++ {
		if !unicode.IsUpper(runes[i]) {
			continue
		}
		if !unicode.IsUpper(runes[i-1]) || (i+1 < len(runes) && unicode.IsLower(runes[i+1])) {
			components = append(components, string(runes[start:i]))
			start = i
		}
	}
	components = append(components, string(runes[start:]))

	return components
}

func snakeSplit(propertyName string) []string {
	return strings.Split(propertyName, "_")
}

func kebabSplit(propertyName string) []string {
	return strings.Split(propertyName, "-")
}

/* Implement static compose functions */

func camelCompose(components []string) string {
	var builder strings.Builder
	for i, component := range components {
		// every word after the first starts with an upper-case character, and the rest of
		// the word keeps its case so that runs of capitals like "HTTP" are preserved
		if i > 0 && len(component) > 0 {
			runes := []rune(component)
			runes[0] = unicode.ToUpper(runes[0])
			component = string(runes)
		}
		builder.WriteString(component)
	}
	return builder.String()
}

func snakeCompose(components []string) string {
	return strings.Join(components, "_")
}

func kebabCompose(components []string) string {
	return strings.Join(components, "-")
}
