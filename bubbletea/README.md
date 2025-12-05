# Bubble Tea Namespace for AgentML

The Bubble Tea namespace lets AgentML documents define interactive terminal UIs directly inside executable content. Use declarative XML to describe menus/lists and receive user interaction as standard AgentML events that can drive SCXML transitions.

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

Declare the namespace and add a `<bubbletea:program>` inside any executable content (e.g., `onentry`):

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
