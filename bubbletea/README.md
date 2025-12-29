# Bubble Tea Namespace for AgentML

The Bubble Tea namespace lets AgentML documents define interactive terminal UIs directly inside executable content. Use declarative XML to describe Bubble Tea components (lists, inputs, tables, etc.) and receive user interaction as standard AgentML events that can drive SCXML transitions.

## Installation

```bash
go get github.com/agentflare-ai/agentml-go/bubbletea
```

Register the namespace loader when constructing an interpreter:

```go
config := interpreter.Config{
    Namespaces: map[string]agentml.NamespaceLoader{
        bubbletea.NamespaceURI: bubbletea.Loader(nil),
    },
}
itp := interpreter.New(ctx, doc, config)
```

## AgentML Usage

Declare the namespace and add a `<bubbletea:program>` inside any executable content (e.g., `onentry`). Each program contains exactly one Bubble Tea component element (for example, `bubbletea:list` or `bubbletea:textinput`):

```xml
<agentml xmlns="github.com/agentflare-ai/agentml"
         xmlns:bubbletea="github.com/agentflare-ai/agentml-go/bubbletea"
         datamodel="ecmascript">

  <datamodel>
    <data id="choice" expr="null" />
  </datamodel>

  <state id="main">
    <onentry>
      <bubbletea:program id="groceries">
        <bubbletea:list title="Pick groceries"
                        multi="true"
                        change-event="ui.change"
                        submit-event="ui.submit"
                        quit-event="ui.quit">
          <bubbletea:item value="apples">Apples</bubbletea:item>
          <bubbletea:item value="bananas">Bananas</bubbletea:item>
          <bubbletea:item value="carrots">Carrots</bubbletea:item>
        </bubbletea:list>
      </bubbletea:program>
    </onentry>

    <transition event="ui.submit" target="done">
      <assign location="choice" expr="_event.data" />
    </transition>

    <transition event="ui.quit" target="done" />
  </state>

  <final id="done" />
</agentml>
```

### Expression Attributes

All component attributes support expression variants using `{{attr}}expr` (or `{{attr}}-expr`) when a datamodel is available. For example, `valueexpr` or `value-expr` can be used in place of `value` to compute a value at runtime. Elements with text content (such as `bubbletea:item` or `bubbletea:cell`) also accept an `expr` attribute to compute their text.

## Supported Components

Each component maps to a Bubbles component from the Charmbracelet ecosystem. The element name matches
the component name (no generic wrapper).

Currently supported:

* `bubbletea:list`
* `bubbletea:textinput`
* `bubbletea:textarea`
* `bubbletea:table`
* `bubbletea:progress`
* `bubbletea:paginator`
* `bubbletea:viewport`
* `bubbletea:spinner`
* `bubbletea:filepicker`
* `bubbletea:timer`
* `bubbletea:stopwatch`

Component payloads always include `{component, programId, componentId, reason}` plus component-
specific fields (e.g., `value`, `cursorIndex`, `row`, `percent`).

### Event Payloads

Every emitted event is an `external` AgentML event with a payload such as:

```json
{
  "component": "list",
  "programId": "groceries",
  "listId": "list-1",
  "selectedIndices": [0, 2],
  "selectedValues": ["apples", "carrots"],
  "selectedLabels": ["Apples", "Carrots"],
  "reason": "submit"
}
```

* `cursor-event`: `{component, programId, listId, cursorIndex, value, label}`
* `change-event`: selection payload with `reason: "change"`
* `submit-event`: selection payload with `reason: "submit"` *(defaults to `bubbletea.submit` if omitted)*
* `quit-event`: selection payload with `reason: "quit"` *(defaults to `bubbletea.quit`)*

## Key Bindings

* `↑ / k`: move cursor up
* `↓ / j`: move cursor down
* `space`: toggle selection (multi-select lists)
* `enter`: submit
* `q` or `ctrl+c`: quit

## Schema

Validation is provided by [`bubbletea.xsd`](bubbletea.xsd). Reference it in editors or CI via `https://xsd.agentml.dev/agentflare-ai/agentml-go/bubbletea/bubbletea.xsd`.
