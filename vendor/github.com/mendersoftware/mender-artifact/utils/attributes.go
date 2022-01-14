// Copyright 2021 Northern.tech AS
//
//    Licensed under the Apache License, Version 2.0 (the "License");
//    you may not use this file except in compliance with the License.
//    You may obtain a copy of the License at
//
//        http://www.apache.org/licenses/LICENSE-2.0
//
//    Unless required by applicable law or agreed to in writing, software
//    distributed under the License is distributed on an "AS IS" BASIS,
//    WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//    See the License for the specific language governing permissions and
//    limitations under the License.

package utils

import (
	"regexp"
	"strings"

	"github.com/pkg/errors"
)

// Given a string list containing attribute names, and a string list containing
// attribute name wildcard expressions, return the list of attributes that match
// at least one of the expressions. Only '*' wildcard is supported at the
// moment, matching any number of characters, including zero.
//
// This is intended to be used to match `artifact_provides` attributes against
// `clears_artifact_provides` attributes.
func StringsMatchingWildcards(attributes, wildcards []string) ([]string, error) {
	regexes := make([](*regexp.Regexp), 0, len(wildcards))

	// Turn into regular expression.
	for _, wildcard := range wildcards {
		b := strings.Builder{}
		b.WriteRune('^')
		for i := 0; i < len(wildcard); i++ {
			switch wildcard[i] {
			case '\\':
				if i+1 >= len(wildcard) {
					return nil, errors.Errorf(
						"Expression cannot end with a backslash: \"%s\"",
						wildcard)
				}
				b.WriteString(wildcard[i : i+2])
				i++

			case '*':
				b.WriteString(".*")

			default:
				b.WriteString(regexp.QuoteMeta(wildcard[i : i+1]))
			}
		}
		b.WriteRune('$')
		regex, err := regexp.Compile(b.String())
		if err != nil {
			return nil, errors.Wrap(err,
				"Wildcard expression resulted in failed regular expression")
		}
		regexes = append(regexes, regex)
	}

	matches := []string{}
	for _, attribute := range attributes {
		for _, regex := range regexes {
			if regex.MatchString(attribute) {
				matches = append(matches, attribute)
				break
			}
		}
	}

	return matches, nil
}
