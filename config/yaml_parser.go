package config

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type containerKind int

const (
	mapKind containerKind = iota
	sliceKind
)

type yamlContainer struct {
	indent      int
	kind        containerKind
	mapRef      map[string]interface{}
	sliceRef    *[]interface{}
	assignSlice func([]interface{})
}

type pendingEntry struct {
	parent map[string]interface{}
	key    string
	indent int
}

func unmarshalYAMLConfig(data []byte, v interface{}) error {
	parsed, err := parseYAML(string(data))
	if err != nil {
		return err
	}

	jsonBytes, err := json.Marshal(parsed)
	if err != nil {
		return err
	}

	return json.Unmarshal(jsonBytes, v)
}

func parseYAML(content string) (map[string]interface{}, error) {
	root := make(map[string]interface{})
	stack := []yamlContainer{{
		indent: -1,
		kind:   mapKind,
		mapRef: root,
	}}

	var pending *pendingEntry

	lines := strings.Split(content, "\n")
	for idx, rawLine := range lines {
		trimmedLine := strings.TrimSpace(rawLine)
		if trimmedLine == "" {
			continue
		}

		trimmedLine = stripInlineComment(trimmedLine)
		if trimmedLine == "" {
			continue
		}

		indent := countIndent(rawLine)
		if indent < 0 {
			return nil, fmt.Errorf("invalid indentation on line %d", idx+1)
		}

		for len(stack) > 0 && indent <= stack[len(stack)-1].indent {
			stack = stack[:len(stack)-1]
		}
		if len(stack) == 0 {
			return nil, fmt.Errorf("invalid indentation structure on line %d", idx+1)
		}

		if pending != nil {
			if indent <= pending.indent {
				pending.parent[pending.key] = map[string]interface{}{}
				pending = nil
			} else {
				if strings.HasPrefix(trimmedLine, "-") {
					newSlice := []interface{}{}
					parent := pending.parent
					key := pending.key
					assign := func(updated []interface{}) {
						parent[key] = updated
					}
					assign(newSlice)
					stack = append(stack, yamlContainer{
						indent:      pending.indent,
						kind:        sliceKind,
						sliceRef:    &newSlice,
						assignSlice: assign,
					})
				} else {
					newMap := map[string]interface{}{}
					pending.parent[pending.key] = newMap
					stack = append(stack, yamlContainer{
						indent: pending.indent,
						kind:   mapKind,
						mapRef: newMap,
					})
				}
				pending = nil
			}
		}

		current := stack[len(stack)-1]

		if strings.HasPrefix(trimmedLine, "-") {
			if current.kind != sliceKind {
				return nil, fmt.Errorf("unexpected list item on line %d", idx+1)
			}

			item := strings.TrimSpace(trimmedLine[1:])
			item = stripInlineComment(item)
			parentAssign := current.assignSlice
			sliceRef := current.sliceRef

			if item == "" {
				newMap := map[string]interface{}{}
				updated := append(*sliceRef, newMap)
				*sliceRef = updated
				parentAssign(updated)
				stack = append(stack, yamlContainer{
					indent: indent,
					kind:   mapKind,
					mapRef: newMap,
				})
				continue
			}

			if idx := strings.Index(item, ":"); idx != -1 {
				if idx != len(item)-1 && !(item[idx+1] == ' ' || item[idx+1] == '\t') {
					// Colon is part of the value (e.g., IPv6 address), treat as scalar.
				} else {
					parts := strings.SplitN(item, ":", 2)
					key := strings.TrimSpace(parts[0])
					valuePart := ""
					if len(parts) > 1 {
						valuePart = strings.TrimSpace(stripInlineComment(parts[1]))
					}

					newMap := map[string]interface{}{}
					if valuePart == "" {
						pending = &pendingEntry{
							parent: newMap,
							key:    key,
							indent: indent,
						}
					} else {
						newMap[key] = parseScalar(valuePart)
					}
					updated := append(*sliceRef, newMap)
					*sliceRef = updated
					parentAssign(updated)
					stack = append(stack, yamlContainer{
						indent: indent,
						kind:   mapKind,
						mapRef: newMap,
					})
					continue
				}
			}

			value := parseScalar(item)
			updated := append(*sliceRef, value)
			*sliceRef = updated
			parentAssign(updated)
			continue
		}

		if current.kind != mapKind {
			return nil, fmt.Errorf("expected map entry on line %d", idx+1)
		}

		parts := strings.SplitN(trimmedLine, ":", 2)
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid map entry on line %d", idx+1)
		}

		key := strings.TrimSpace(parts[0])
		valuePart := strings.TrimSpace(stripInlineComment(parts[1]))

		if valuePart == "" {
			pending = &pendingEntry{
				parent: current.mapRef,
				key:    key,
				indent: indent,
			}
			continue
		}

		current.mapRef[key] = parseScalar(valuePart)
	}

	if pending != nil {
		pending.parent[pending.key] = map[string]interface{}{}
	}

	return root, nil
}

func parseScalar(value string) interface{} {
	if value == "" {
		return ""
	}

	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			return value[1 : len(value)-1]
		}
	}

	lower := strings.ToLower(value)
	switch lower {
	case "true", "yes", "on":
		return true
	case "false", "no", "off":
		return false
	case "null", "~":
		return nil
	}

	if i, err := strconv.Atoi(value); err == nil {
		return i
	}

	return value
}

func countIndent(line string) int {
	count := 0
	for _, r := range line {
		if r == ' ' {
			count++
			continue
		}
		if r == '\t' {
			return -1
		}
		break
	}
	return count
}

func stripInlineComment(line string) string {
	inSingle := false
	inDouble := false
	for i := 0; i < len(line); i++ {
		ch := line[i]
		switch ch {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return strings.TrimSpace(line[:i])
			}
		}
	}
	return strings.TrimSpace(line)
}
