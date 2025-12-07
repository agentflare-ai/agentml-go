package validator

import (
	"github.com/agentflare-ai/go-xmldom"
)

// SemanticRule validates SCXML-specific semantic constraints that cannot be
// expressed in XSD schema (e.g., mutual exclusion, context-dependent rules).
type SemanticRule interface {
	// Name returns the diagnostic code for this rule (e.g., "E301")
	Name() string

	// Validate checks the rule against a document and returns diagnostics
	Validate(doc xmldom.Document, config Config) []Diagnostic
}

// DefaultSemanticRules returns the standard SCXML 1.0 semantic validators.
// These rules enforce constraints from the SCXML specification that cannot
// be expressed in XSD schemas alone.
func DefaultSemanticRules() []SemanticRule {
	return []SemanticRule{
		// Format/Token validation
		&IDTokenRule{},
		&EventDescriptorRule{},

		// Mutual exclusion (XOR constraints)
		&ParamNameAndXorRule{},
		&CancelExactlyOneRule{},
		&SendContentEventExclusionRule{},
		&SendNamelistContentExclusionRule{},
		&InvokeSrcExclusivityRule{},
		&DonedataContentParamExclusionRule{},

		// Cardinality constraints
		&InitialOneTransitionRule{},

		// Context-dependent (tree relationship) rules
		&InitialTransitionConstraintsRule{},
		&InitialTargetDescendantRule{},
		&HistoryShallowTargetRule{},
		&TransitionAtLeastOneRule{},
		&StateInitialConflictRule{},
		&StateInitialAtomicRule{},

		// Liveness / Reachability rules
		&StateDeadlockRule{},
		&UnconditionalTransitionCycleRule{},
	}
}
