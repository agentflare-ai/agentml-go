package validator

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/agentflare-ai/go-xmldom"
)

// ============================================================================
// Format/Token Validation Rules (E301-E309)
// ============================================================================

// IDTokenRule validates that ID attributes are valid NCName tokens
type IDTokenRule struct{}

func (r *IDTokenRule) Name() string { return "E301" }

func (r *IDTokenRule) Validate(doc xmldom.Document, config Config) []Diagnostic {
	var diags []Diagnostic
	root := doc.DocumentElement()
	if root == nil {
		return diags
	}

	// NCName pattern: must start with letter/underscore, no colons allowed
	ncnamePattern := regexp.MustCompile(`^[A-Za-z_][\w.-]*$`)

	walkElements(root, func(elem xmldom.Element) {
		if id := string(elem.GetAttribute("id")); id != "" {
			if !ncnamePattern.MatchString(id) {
				line, col, off := elem.Position()
				diags = append(diags, Diagnostic{
					Severity: SeverityError,
					Code:     "E301",
					Message:  fmt.Sprintf("ID '%s' is not a valid NCName token", id),
					Position: Position{
						File:   config.SourceName,
						Line:   line,
						Column: col,
						Offset: off,
					},
					Tag:       string(elem.LocalName()),
					Attribute: "id",
					Hints: []string{
						"IDs must start with a letter or underscore",
						"IDs cannot start with a digit",
						"IDs cannot contain colons",
					},
				})
			}
		}
	})

	return diags
}

// EventDescriptorRule validates event descriptor tokens
type EventDescriptorRule struct{}

func (r *EventDescriptorRule) Name() string { return "E302" }

func (r *EventDescriptorRule) Validate(doc xmldom.Document, config Config) []Diagnostic {
	var diags []Diagnostic
	root := doc.DocumentElement()
	if root == nil {
		return diags
	}

	// Event tokens are space-separated, each must be valid
	// Valid event: word characters, dots, dashes, but not commas
	eventTokenPattern := regexp.MustCompile(`^[\w.-]+$`)

	walkElements(root, func(elem xmldom.Element) {
		if string(elem.LocalName()) == "transition" {
			if event := string(elem.GetAttribute("event")); event != "" {
				tokens := strings.Fields(event)
				for _, token := range tokens {
					if !eventTokenPattern.MatchString(token) || strings.Contains(token, ",") {
						line, col, off := elem.Position()
						diags = append(diags, Diagnostic{
							Severity: SeverityError,
							Code:     "E302",
							Message:  fmt.Sprintf("Event token '%s' is invalid", token),
							Position: Position{
								File:   config.SourceName,
								Line:   line,
								Column: col,
								Offset: off,
							},
							Tag:       "transition",
							Attribute: "event",
							Hints: []string{
								"Event tokens must be space-separated (not comma-separated)",
								"Each token can contain letters, digits, dots, underscores, and hyphens",
							},
						})
					}
				}
			}
		}
	})

	return diags
}

// ============================================================================
// Mutual Exclusion Rules (E310-E319)
// ============================================================================

// ParamNameAndXorRule validates <param> requires name and exactly one of expr/location
type ParamNameAndXorRule struct{}

func (r *ParamNameAndXorRule) Name() string { return "E310" }

func (r *ParamNameAndXorRule) Validate(doc xmldom.Document, config Config) []Diagnostic {
	var diags []Diagnostic
	root := doc.DocumentElement()
	if root == nil {
		return diags
	}

	walkElements(root, func(elem xmldom.Element) {
		if string(elem.LocalName()) == "param" {
			name := string(elem.GetAttribute("name"))
			expr := string(elem.GetAttribute("expr"))
			location := string(elem.GetAttribute("location"))

			line, col, off := elem.Position()

			if name == "" {
				diags = append(diags, Diagnostic{
					Severity: SeverityError,
					Code:     "E310",
					Message:  "<param> must have a 'name' attribute",
					Position: Position{
						File:   config.SourceName,
						Line:   line,
						Column: col,
						Offset: off,
					},
					Tag: "param",
					Hints: []string{
						"The 'name' attribute is required for <param> elements",
					},
				})
			}

			hasExpr := expr != ""
			hasLocation := location != ""

			if !hasExpr && !hasLocation {
				diags = append(diags, Diagnostic{
					Severity: SeverityError,
					Code:     "E310",
					Message:  "<param> must specify exactly one of 'expr' or 'location'",
					Position: Position{
						File:   config.SourceName,
						Line:   line,
						Column: col,
						Offset: off,
					},
					Tag: "param",
					Hints: []string{
						"Specify either 'expr' or 'location', but not both",
					},
				})
			}

			if hasExpr && hasLocation {
				diags = append(diags, Diagnostic{
					Severity: SeverityError,
					Code:     "E310",
					Message:  "<param> cannot have both 'expr' and 'location'",
					Position: Position{
						File:   config.SourceName,
						Line:   line,
						Column: col,
						Offset: off,
					},
					Tag:       "param",
					Attribute: "expr",
					Hints: []string{
						"Use 'expr' for a value expression OR 'location' for a datamodel location",
						"These attributes are mutually exclusive",
					},
				})
			}
		}
	})

	return diags
}

// CancelExactlyOneRule validates <cancel> requires exactly one of sendid/sendidexpr
type CancelExactlyOneRule struct{}

func (r *CancelExactlyOneRule) Name() string { return "E311" }

func (r *CancelExactlyOneRule) Validate(doc xmldom.Document, config Config) []Diagnostic {
	var diags []Diagnostic
	root := doc.DocumentElement()
	if root == nil {
		return diags
	}

	walkElements(root, func(elem xmldom.Element) {
		if string(elem.LocalName()) == "cancel" {
			sendid := string(elem.GetAttribute("sendid"))
			sendidexpr := string(elem.GetAttribute("sendidexpr"))

			hasSendid := sendid != ""
			hasSendidexpr := sendidexpr != ""

			if !hasSendid && !hasSendidexpr {
				line, col, off := elem.Position()
				diags = append(diags, Diagnostic{
					Severity: SeverityError,
					Code:     "E311",
					Message:  "<cancel> must specify exactly one of 'sendid' or 'sendidexpr'",
					Position: Position{
						File:   config.SourceName,
						Line:   line,
						Column: col,
						Offset: off,
					},
					Tag: "cancel",
					Hints: []string{
						"Specify either 'sendid' or 'sendidexpr', but not both",
					},
				})
			}

			if hasSendid && hasSendidexpr {
				line, col, off := elem.Position()
				diags = append(diags, Diagnostic{
					Severity: SeverityError,
					Code:     "E311",
					Message:  "<cancel> cannot have both 'sendid' and 'sendidexpr'",
					Position: Position{
						File:   config.SourceName,
						Line:   line,
						Column: col,
						Offset: off,
					},
					Tag:       "cancel",
					Attribute: "sendid",
					Hints: []string{
						"These attributes are mutually exclusive",
					},
				})
			}
		}
	})

	return diags
}

// SendContentEventExclusionRule validates <send> with content cannot have event/eventexpr
type SendContentEventExclusionRule struct{}

func (r *SendContentEventExclusionRule) Name() string { return "E312" }

func (r *SendContentEventExclusionRule) Validate(doc xmldom.Document, config Config) []Diagnostic {
	var diags []Diagnostic
	root := doc.DocumentElement()
	if root == nil {
		return diags
	}

	walkElements(root, func(elem xmldom.Element) {
		if string(elem.LocalName()) == "send" {
			event := string(elem.GetAttribute("event"))
			eventexpr := string(elem.GetAttribute("eventexpr"))

			hasEvent := event != "" || eventexpr != ""
			hasContent := elementHasChild(elem, "content")

			if hasEvent && hasContent {
				line, col, off := elem.Position()
				diags = append(diags, Diagnostic{
					Severity: SeverityError,
					Code:     "E312",
					Message:  "<send> with <content> child cannot have 'event' or 'eventexpr' attributes",
					Position: Position{
						File:   config.SourceName,
						Line:   line,
						Column: col,
						Offset: off,
					},
					Tag:       "send",
					Attribute: "event",
					Hints: []string{
						"Use either event attributes OR <content> child, not both",
					},
				})
			}
		}
	})

	return diags
}

// SendNamelistContentExclusionRule validates <send> with namelist cannot have content/param
type SendNamelistContentExclusionRule struct{}

func (r *SendNamelistContentExclusionRule) Name() string { return "E313" }

func (r *SendNamelistContentExclusionRule) Validate(doc xmldom.Document, config Config) []Diagnostic {
	var diags []Diagnostic
	root := doc.DocumentElement()
	if root == nil {
		return diags
	}

	walkElements(root, func(elem xmldom.Element) {
		if string(elem.LocalName()) == "send" {
			namelist := string(elem.GetAttribute("namelist"))
			hasNamelist := namelist != ""

			hasContent := elementHasChild(elem, "content")
			hasParam := elementHasChild(elem, "param")

			if hasNamelist && (hasContent || hasParam) {
				line, col, off := elem.Position()
				diags = append(diags, Diagnostic{
					Severity: SeverityError,
					Code:     "E313",
					Message:  "<send> with 'namelist' cannot have <content> or <param> children",
					Position: Position{
						File:   config.SourceName,
						Line:   line,
						Column: col,
						Offset: off,
					},
					Tag:       "send",
					Attribute: "namelist",
					Hints: []string{
						"Use either 'namelist' OR <content>/<param> children, not both",
					},
				})
			}
		}
	})

	return diags
}

// InvokeSrcExclusivityRule validates <invoke> cannot have both src and srcexpr
type InvokeSrcExclusivityRule struct{}

func (r *InvokeSrcExclusivityRule) Name() string { return "E314" }

func (r *InvokeSrcExclusivityRule) Validate(doc xmldom.Document, config Config) []Diagnostic {
	var diags []Diagnostic
	root := doc.DocumentElement()
	if root == nil {
		return diags
	}

	walkElements(root, func(elem xmldom.Element) {
		if string(elem.LocalName()) == "invoke" {
			src := string(elem.GetAttribute("src"))
			srcexpr := string(elem.GetAttribute("srcexpr"))

			if src != "" && srcexpr != "" {
				line, col, off := elem.Position()
				diags = append(diags, Diagnostic{
					Severity: SeverityError,
					Code:     "E314",
					Message:  "<invoke> cannot have both 'src' and 'srcexpr' attributes",
					Position: Position{
						File:   config.SourceName,
						Line:   line,
						Column: col,
						Offset: off,
					},
					Tag:       "invoke",
					Attribute: "src",
					Hints: []string{
						"Use either 'src' for a literal URI OR 'srcexpr' for a computed URI",
						"These attributes are mutually exclusive",
					},
				})
			}
		}
	})

	return diags
}

// DonedataContentParamExclusionRule validates <donedata> cannot have both content and param
type DonedataContentParamExclusionRule struct{}

func (r *DonedataContentParamExclusionRule) Name() string { return "E315" }

func (r *DonedataContentParamExclusionRule) Validate(doc xmldom.Document, config Config) []Diagnostic {
	var diags []Diagnostic
	root := doc.DocumentElement()
	if root == nil {
		return diags
	}

	walkElements(root, func(elem xmldom.Element) {
		if string(elem.LocalName()) == "donedata" {
			hasContent := elementHasChild(elem, "content")
			hasParam := elementHasChild(elem, "param")

			if hasContent && hasParam {
				line, col, off := elem.Position()
				diags = append(diags, Diagnostic{
					Severity: SeverityError,
					Code:     "E315",
					Message:  "<donedata> cannot have both <content> and <param> children",
					Position: Position{
						File:   config.SourceName,
						Line:   line,
						Column: col,
						Offset: off,
					},
					Tag: "donedata",
					Hints: []string{
						"Use either <content> OR <param> children, not both",
					},
				})
			}
		}
	})

	return diags
}

// ============================================================================
// Cardinality Rules (E320-E329)
// ============================================================================

// InitialOneTransitionRule validates <initial> must have exactly one <transition>
type InitialOneTransitionRule struct{}

func (r *InitialOneTransitionRule) Name() string { return "E320" }

func (r *InitialOneTransitionRule) Validate(doc xmldom.Document, config Config) []Diagnostic {
	var diags []Diagnostic
	root := doc.DocumentElement()
	if root == nil {
		return diags
	}

	walkElements(root, func(elem xmldom.Element) {
		if string(elem.LocalName()) == "initial" {
			transitionCount := countChildElements(elem, "transition")

			if transitionCount != 1 {
				line, col, off := elem.Position()
				diags = append(diags, Diagnostic{
					Severity: SeverityError,
					Code:     "E320",
					Message:  fmt.Sprintf("<initial> must have exactly one <transition> child (found %d)", transitionCount),
					Position: Position{
						File:   config.SourceName,
						Line:   line,
						Column: col,
						Offset: off,
					},
					Tag: "initial",
					Hints: []string{
						"The <initial> element requires exactly one <transition> child",
					},
				})
			}
		}
	})

	return diags
}

// ============================================================================
// Context-Dependent Rules (E330-E339)
// ============================================================================

// InitialTransitionConstraintsRule validates <initial>'s <transition> cannot have event/cond
type InitialTransitionConstraintsRule struct{}

func (r *InitialTransitionConstraintsRule) Name() string { return "E330" }

func (r *InitialTransitionConstraintsRule) Validate(doc xmldom.Document, config Config) []Diagnostic {
	var diags []Diagnostic
	root := doc.DocumentElement()
	if root == nil {
		return diags
	}

	walkElements(root, func(elem xmldom.Element) {
		if string(elem.LocalName()) == "initial" {
			// Find transition children
			children := elem.Children()
			for i := uint(0); i < children.Length(); i++ {
				if child := children.Item(i); child != nil {
					if string(child.LocalName()) == "transition" {
						event := string(child.GetAttribute("event"))
						cond := string(child.GetAttribute("cond"))

						if event != "" || cond != "" {
							line, col, off := child.Position()
							diags = append(diags, Diagnostic{
								Severity: SeverityError,
								Code:     "E330",
								Message:  "<transition> inside <initial> cannot have 'event' or 'cond' attributes",
								Position: Position{
									File:   config.SourceName,
									Line:   line,
									Column: col,
									Offset: off,
								},
								Tag:       "transition",
								Attribute: "event",
								Hints: []string{
									"Initial transitions are unconditional",
									"Remove 'event' and 'cond' attributes from this transition",
								},
							})
						}

						// Also check for missing target
						target := string(child.GetAttribute("target"))
						if target == "" {
							line, col, off := child.Position()
							diags = append(diags, Diagnostic{
								Severity: SeverityError,
								Code:     "E330",
								Message:  "<transition> inside <initial> must have a 'target' attribute",
								Position: Position{
									File:   config.SourceName,
									Line:   line,
									Column: col,
									Offset: off,
								},
								Tag: "transition",
								Hints: []string{
									"Initial transitions must specify which state to enter",
								},
							})
						}
					}
				}
			}
		}
	})

	return diags
}

// InitialTargetDescendantRule validates <initial>'s target must be a descendant
type InitialTargetDescendantRule struct{}

func (r *InitialTargetDescendantRule) Name() string { return "E331" }

func (r *InitialTargetDescendantRule) Validate(doc xmldom.Document, config Config) []Diagnostic {
	var diags []Diagnostic
	root := doc.DocumentElement()
	if root == nil {
		return diags
	}

	// Build ID map
	idMap := buildIDMap(root)

	walkElements(root, func(elem xmldom.Element) {
		if string(elem.LocalName()) == "initial" {
			// Find parent state
			parent := elem.ParentNode()
			if parent == nil {
				return
			}

			// Find transition child
			children := elem.Children()
			for i := uint(0); i < children.Length(); i++ {
				if child := children.Item(i); child != nil {
					if string(child.LocalName()) == "transition" {
						target := string(child.GetAttribute("target"))
						if target == "" {
							continue
						}

						// Check if target is a descendant of parent
						targetElem, exists := idMap[target]
						if !exists {
							continue // IDREF validation will catch this
						}

						if !isDescendantOf(targetElem, parent) {
							line, col, off := child.Position()
							diags = append(diags, Diagnostic{
								Severity: SeverityError,
								Code:     "E331",
								Message:  fmt.Sprintf("Initial transition target '%s' must be a descendant of the parent state", target),
								Position: Position{
									File:   config.SourceName,
									Line:   line,
									Column: col,
									Offset: off,
								},
								Tag:       "transition",
								Attribute: "target",
								Hints: []string{
									"Initial transitions can only target child states",
								},
							})
						}
					}
				}
			}
		}
	})

	return diags
}

// HistoryShallowTargetRule validates shallow history target must be immediate child
type HistoryShallowTargetRule struct{}

func (r *HistoryShallowTargetRule) Name() string { return "E332" }

func (r *HistoryShallowTargetRule) Validate(doc xmldom.Document, config Config) []Diagnostic {
	var diags []Diagnostic
	root := doc.DocumentElement()
	if root == nil {
		return diags
	}

	// Build ID map
	idMap := buildIDMap(root)

	walkElements(root, func(elem xmldom.Element) {
		if string(elem.LocalName()) == "history" {
			historyType := string(elem.GetAttribute("type"))
			if historyType == "" {
				historyType = "shallow" // default
			}

			if historyType != "shallow" {
				return
			}

			// Find parent state
			parent := elem.ParentNode()
			if parent == nil {
				return
			}

			// Find transition child
			children := elem.Children()
			for i := uint(0); i < children.Length(); i++ {
				if child := children.Item(i); child != nil {
					if string(child.LocalName()) == "transition" {
						target := string(child.GetAttribute("target"))
						if target == "" {
							continue
						}

						// Check if target is immediate child of parent
						targetElem, exists := idMap[target]
						if !exists {
							continue // IDREF validation will catch this
						}

						if !isImmediateChildOf(targetElem, parent) {
							line, col, off := child.Position()
							diags = append(diags, Diagnostic{
								Severity: SeverityError,
								Code:     "E332",
								Message:  fmt.Sprintf("Shallow history target '%s' must be an immediate child of the parent state", target),
								Position: Position{
									File:   config.SourceName,
									Line:   line,
									Column: col,
									Offset: off,
								},
								Tag:       "transition",
								Attribute: "target",
								Hints: []string{
									"Shallow history can only target direct children",
									"Use type='deep' to target deeper descendants",
								},
							})
						}
					}
				}
			}
		}
	})

	return diags
}

// TransitionAtLeastOneRule validates transition must specify event/cond/target
type TransitionAtLeastOneRule struct{}

func (r *TransitionAtLeastOneRule) Name() string { return "E333" }

func (r *TransitionAtLeastOneRule) Validate(doc xmldom.Document, config Config) []Diagnostic {
	var diags []Diagnostic
	root := doc.DocumentElement()
	if root == nil {
		return diags
	}

	walkElements(root, func(elem xmldom.Element) {
		if string(elem.LocalName()) == "transition" {
			// Skip if inside <initial> - different rules apply
			parent := elem.ParentNode()
			if parent != nil && string(parent.LocalName()) == "initial" {
				return
			}

			event := string(elem.GetAttribute("event"))
			cond := string(elem.GetAttribute("cond"))
			target := string(elem.GetAttribute("target"))

			if event == "" && cond == "" && target == "" {
				line, col, off := elem.Position()
				diags = append(diags, Diagnostic{
					Severity: SeverityError,
					Code:     "E333",
					Message:  "<transition> must specify at least one of 'event', 'cond', or 'target'",
					Position: Position{
						File:   config.SourceName,
						Line:   line,
						Column: col,
						Offset: off,
					},
					Tag: "transition",
					Hints: []string{
						"A transition needs at least one of: event trigger, condition, or target state",
					},
				})
			}
		}
	})

	return diags
}

// StateInitialConflictRule validates state cannot have both initial attribute and element
type StateInitialConflictRule struct{}

func (r *StateInitialConflictRule) Name() string { return "E334" }

func (r *StateInitialConflictRule) Validate(doc xmldom.Document, config Config) []Diagnostic {
	var diags []Diagnostic
	root := doc.DocumentElement()
	if root == nil {
		return diags
	}

	walkElements(root, func(elem xmldom.Element) {
		tagName := string(elem.LocalName())
		if tagName == "state" || tagName == "scxml" {
			initialAttr := string(elem.GetAttribute("initial"))
			hasInitialElement := elementHasChild(elem, "initial")

			if initialAttr != "" && hasInitialElement {
				line, col, off := elem.Position()
				diags = append(diags, Diagnostic{
					Severity: SeverityError,
					Code:     "E334",
					Message:  fmt.Sprintf("<%s> cannot have both 'initial' attribute and <initial> child element", tagName),
					Position: Position{
						File:   config.SourceName,
						Line:   line,
						Column: col,
						Offset: off,
					},
					Tag:       tagName,
					Attribute: "initial",
					Hints: []string{
						"Use either the 'initial' attribute OR an <initial> child element, not both",
					},
				})
			}
		}
	})

	return diags
}

// StateInitialAtomicRule validates atomic state cannot have initial attribute
type StateInitialAtomicRule struct{}

func (r *StateInitialAtomicRule) Name() string { return "E335" }

func (r *StateInitialAtomicRule) Validate(doc xmldom.Document, config Config) []Diagnostic {
	var diags []Diagnostic
	root := doc.DocumentElement()
	if root == nil {
		return diags
	}

	walkElements(root, func(elem xmldom.Element) {
		if string(elem.LocalName()) == "state" {
			initialAttr := string(elem.GetAttribute("initial"))
			if initialAttr == "" {
				return
			}

			// Check if atomic (no child states)
			isAtomic := true
			children := elem.Children()
			for i := uint(0); i < children.Length(); i++ {
				if child := children.Item(i); child != nil {
					childTag := string(child.LocalName())
					if childTag == "state" || childTag == "parallel" || childTag == "final" {
						isAtomic = false
						break
					}
				}
			}

			if isAtomic {
				line, col, off := elem.Position()
				diags = append(diags, Diagnostic{
					Severity: SeverityError,
					Code:     "E335",
					Message:  "Atomic <state> (no child states) cannot have 'initial' attribute",
					Position: Position{
						File:   config.SourceName,
						Line:   line,
						Column: col,
						Offset: off,
					},
					Tag:       "state",
					Attribute: "initial",
					Hints: []string{
						"The 'initial' attribute is only valid for compound states with children",
						"Remove the 'initial' attribute or add child states",
					},
				})
			}
		}
	})

	return diags
}

// ============================================================================
// Liveness / Reachability Rules (E340-E349)
// ============================================================================

// StateDeadlockRule validates non-final states have at least one unconditional exit path
type StateDeadlockRule struct{}

func (r *StateDeadlockRule) Name() string { return "W340" }

func (r *StateDeadlockRule) Validate(doc xmldom.Document, config Config) []Diagnostic {
	var diags []Diagnostic
	root := doc.DocumentElement()
	if root == nil {
		return diags
	}

	walkElements(root, func(elem xmldom.Element) {
		tagName := string(elem.LocalName())

		// Only check atomic states (states without child states) and non-final states
		if tagName != "state" {
			return
		}

		// Check if this is an atomic state (no child states)
		// Also check if this state has <invoke> children
		isAtomic := true
		hasInvoke := false
		children := elem.Children()
		for i := uint(0); i < children.Length(); i++ {
			if child := children.Item(i); child != nil {
				childTag := string(child.LocalName())
				if childTag == "state" || childTag == "parallel" || childTag == "final" {
					isAtomic = false
					break
				}
				if childTag == "invoke" {
					hasInvoke = true
				}
			}
		}

		// Only check atomic states
		if !isAtomic {
			return
		}

		// Skip states with <invoke> - these follow the invoke pattern where
		// they wait for done.invoke.* or error.invoke.* events
		if hasInvoke {
			return
		}

		// Count transitions and check if all are conditional
		hasTransition := false
		allTransitionsConditional := true

		for i := uint(0); i < children.Length(); i++ {
			if child := children.Item(i); child != nil {
				if string(child.LocalName()) == "transition" {
					hasTransition = true

					// Check if this transition is unconditional
					// A transition is unconditional if it has no event and no cond
					event := string(child.GetAttribute("event"))
					cond := string(child.GetAttribute("cond"))

					if event == "" && cond == "" {
						// Found an unconditional transition
						allTransitionsConditional = false
						break
					}
				}
			}
		}

		// If state has transitions and all are conditional, warn about potential deadlock
		if hasTransition && allTransitionsConditional {
			stateID := string(elem.GetAttribute("id"))
			line, col, off := elem.Position()
			diags = append(diags, Diagnostic{
				Severity: SeverityWarning,
				Code:     "W340",
				Message:  fmt.Sprintf("State '%s' has only conditional transitions and may deadlock if no events match", stateID),
				Position: Position{
					File:   config.SourceName,
					Line:   line,
					Column: col,
					Offset: off,
				},
				Tag: "state",
				Hints: []string{
					"Add an unconditional fallback transition (without 'event' or 'cond' attributes)",
					"Or ensure all possible events are handled",
					"Example: <transition target=\"fallback_state\" />",
				},
			})
		}
	})

	return diags
}

// UnconditionalTransitionCycleRule detects cycles in unconditional transitions
type UnconditionalTransitionCycleRule struct{}

func (r *UnconditionalTransitionCycleRule) Name() string { return "E341" }

func (r *UnconditionalTransitionCycleRule) Validate(doc xmldom.Document, config Config) []Diagnostic {
	var diags []Diagnostic
	root := doc.DocumentElement()
	if root == nil {
		return diags
	}

	// Build ID map and transition graph
	idMap := buildIDMap(root)
	transitionGraph := buildUnconditionalTransitionGraph(root)

	// For each state, check if following unconditional transitions leads to a cycle
	for stateID := range transitionGraph {
		visited := make(map[string]bool)
		path := []string{stateID}

		if hasCycle := detectCycleFrom(stateID, transitionGraph, visited, path); hasCycle {
			// Found a cycle - report it on the first state in the cycle
			stateElem := idMap[stateID]
			if stateElem != nil {
				line, col, off := stateElem.Position()
				diags = append(diags, Diagnostic{
					Severity: SeverityError,
					Code:     "E341",
					Message:  fmt.Sprintf("State '%s' is part of an unconditional transition cycle: %s", stateID, formatCyclePath(path)),
					Position: Position{
						File:   config.SourceName,
						Line:   line,
						Column: col,
						Offset: off,
					},
					Tag: "state",
					Hints: []string{
						"Unconditional transitions create an infinite loop",
						"Add an 'event' or 'cond' attribute to break the cycle",
						"Or remove one of the unconditional transitions",
					},
				})
			}
			// Only report each cycle once
			break
		}
	}

	return diags
}

// buildUnconditionalTransitionGraph builds a graph of states connected by unconditional transitions
func buildUnconditionalTransitionGraph(root xmldom.Element) map[string][]string {
	graph := make(map[string][]string)

	walkElements(root, func(elem xmldom.Element) {
		if string(elem.LocalName()) != "transition" {
			return
		}

		// Only process unconditional transitions
		event := string(elem.GetAttribute("event"))
		cond := string(elem.GetAttribute("cond"))
		if event != "" || cond != "" {
			return
		}

		// Get source state (parent of transition)
		parent := elem.ParentNode()
		if parent == nil {
			return
		}
		parentElem, ok := parent.(xmldom.Element)
		if !ok {
			return
		}
		sourceID := string(parentElem.GetAttribute("id"))
		if sourceID == "" {
			return
		}

		// Get target state(s)
		target := string(elem.GetAttribute("target"))
		if target == "" {
			return
		}

		// Handle space-separated targets
		targets := strings.Fields(target)
		graph[sourceID] = append(graph[sourceID], targets...)
	})

	return graph
}

// detectCycleFrom performs DFS to detect if there's a cycle reachable from the given state
func detectCycleFrom(current string, graph map[string][]string, visited map[string]bool, path []string) bool {
	if visited[current] {
		// Found a cycle - check if current is in the path
		for _, state := range path {
			if state == current {
				return true
			}
		}
		return false
	}

	visited[current] = true

	// Follow unconditional transitions
	for _, next := range graph[current] {
		newPath := append(path, next)
		if detectCycleFrom(next, graph, visited, newPath) {
			return true
		}
	}

	return false
}

// formatCyclePath formats the cycle path for display
func formatCyclePath(path []string) string {
	if len(path) <= 1 {
		return path[0]
	}
	return strings.Join(path, " → ")
}

// ============================================================================
// Helper Functions
// ============================================================================

// walkElements recursively walks all elements in the tree
func walkElements(elem xmldom.Element, fn func(xmldom.Element)) {
	if elem == nil {
		return
	}
	fn(elem)
	children := elem.Children()
	for i := uint(0); i < children.Length(); i++ {
		if child := children.Item(i); child != nil {
			walkElements(child, fn)
		}
	}
}

// elementHasChild checks if element has a child with the given tag name
func elementHasChild(elem xmldom.Element, childName string) bool {
	children := elem.Children()
	for i := uint(0); i < children.Length(); i++ {
		if child := children.Item(i); child != nil {
			if string(child.LocalName()) == childName {
				return true
			}
		}
	}
	return false
}

// countChildElements counts children with the given tag name
func countChildElements(elem xmldom.Element, childName string) int {
	count := 0
	children := elem.Children()
	for i := uint(0); i < children.Length(); i++ {
		if child := children.Item(i); child != nil {
			if string(child.LocalName()) == childName {
				count++
			}
		}
	}
	return count
}

// buildIDMap creates a map of id -> element
func buildIDMap(root xmldom.Element) map[string]xmldom.Element {
	idMap := make(map[string]xmldom.Element)
	walkElements(root, func(elem xmldom.Element) {
		if id := string(elem.GetAttribute("id")); id != "" {
			idMap[id] = elem
		}
	})
	return idMap
}

// isDescendantOf checks if elem is a descendant of ancestor
func isDescendantOf(elem xmldom.Node, ancestor xmldom.Node) bool {
	current := elem.ParentNode()
	for current != nil {
		if current == ancestor {
			return true
		}
		current = current.ParentNode()
	}
	return false
}

// isImmediateChildOf checks if elem is a direct child of parent
func isImmediateChildOf(elem xmldom.Node, parent xmldom.Node) bool {
	return elem.ParentNode() == parent
}
