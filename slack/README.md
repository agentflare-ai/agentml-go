# Slack Namespace for AgentML

The Slack namespace lets AgentML documents send messages to Slack channels and users, and receive Slack events as AgentML events. Use declarative XML to send messages and configure event handling.

## Installation

```bash
go get github.com/agentflare-ai/agentml-go/slack
```

Register the namespace loader when constructing an interpreter:

```go
config := interpreter.Config{
    Namespaces: map[string]agentml.NamespaceLoader{
        slack.NamespaceURI: slack.Loader(nil),
    },
}
itp := interpreter.New(ctx, doc, config)
```

## Configuration

The Slack namespace requires a Slack Bot Token:

```bash
export SLACK_BOT_TOKEN=xoxb-your-token-here
```

Alternatively, provide a custom client via `Deps`:

```go
client, _ := slack.NewClient(ctx)
deps := slack.Deps{
    Client: client,
}
loader := slack.Loader(deps)
```

## AgentML Usage

### Sending Messages

Declare the namespace and use `<slack:send>` inside any executable content (e.g., `onentry`):

```xml
<agentml xmlns="github.com/agentflare-ai/agentml"
         xmlns:slack="github.com/agentflare-ai/agentml-go/slack"
         datamodel="ecmascript">

  <datamodel>
    <data id="channel" expr="'C1234567890'" />
    <data id="message" expr="'Hello from AgentML!'" />
  </datamodel>

  <state id="send_message">
    <onentry>
      <slack:send channel-expr="channel"
                  text-expr="message"
                  event="slack.message.sent" />
    </onentry>

    <transition event="slack.message.sent" target="done" />
  </state>

  <final id="done" />
</agentml>
```

### Attributes

* **`channel`** or **`channel-expr`**: Channel ID (e.g., `C1234567890`) or name (e.g., `#general`) to send the message to. Mutually exclusive with `user`/`user-expr`.
* **`user`** or **`user-expr`**: User ID (e.g., `U1234567890`) to send a direct message to. Mutually exclusive with `channel`/`channel-expr`.
* **`text`** or **`text-expr`**: Plain text message content. Mutually exclusive.
* **`thread_ts`** or **`thread-expr`**: Thread timestamp (TS) to reply to an existing message. Mutually exclusive.
* **`blocks-expr`**: Expression that evaluates to a Slack Block Kit array. Each block should be a map/object with `type` and other block-specific fields. When provided, blocks take precedence over text.
* **`event`**: Event name to emit when the message is successfully sent. Defaults to `slack.message.sent`.

### Event Payloads

When a message is sent, an event is emitted with a payload such as:

```json
{
  "component": "slack",
  "action": "send",
  "channel": "C1234567890",
  "ts": "1234567890.123456",
  "ok": true
}
```

### Examples

#### Simple Text Message

```xml
<slack:send channel="C1234567890" text="Hello, world!" />
```

#### Using Expressions

```xml
<slack:send channel-expr="targetChannel"
            text-expr="'Message: ' + userInput" />
```

#### Thread Reply

```xml
<slack:send channel="C1234567890"
            text="This is a reply"
            thread_ts="1234567890.123456" />
```

#### Direct Message

```xml
<slack:send user="U1234567890" text="Hello!" />
```

## Receiving Slack Events

To receive Slack events, use the `Router` in your host application:

```go
import (
    "github.com/agentflare-ai/agentml-go/slack"
    "net/http"
)

// Create router with signing secret
router := slack.NewRouter("your-signing-secret", interpreter)

// Mount HTTP handler
http.Handle("/slack/events", router.HTTPHandler())
http.ListenAndServe(":8080", nil)
```

### Event Mapping

Slack events are automatically mapped to AgentML event names:

* `message` → `slack.message.posted`
* `reaction_added` → `slack.reaction.added`
* `reaction_removed` → `slack.reaction.removed`
* `app_mention` → `slack.app.mention`
* Other types → `slack.{type}`

### Event Payloads

Incoming Slack events include:

```json
{
  "component": "slack",
  "slack": {
    "type": "message",
    "event_ts": "1234567890.123456",
    "user": "U1234567890",
    "text": "Hello!",
    "channel": "C1234567890",
    "ts": "1234567890.123456"
  },
  "channel": "C1234567890",
  "user": "U1234567890",
  "text": "Hello!",
  "ts": "1234567890.123456"
}
```

### Handling Events in AgentML

```xml
<agentml xmlns="github.com/agentflare-ai/agentml"
         xmlns:slack="github.com/agentflare-ai/agentml-go/slack"
         datamodel="ecmascript">

  <datamodel>
    <data id="lastMessage" expr="null" />
  </datamodel>

  <state id="listening">
    <transition event="slack.message.posted" target="process">
      <assign location="lastMessage" expr="_event.data.text" />
    </transition>
  </state>

  <state id="process">
    <onentry>
      <slack:send channel-expr="_event.data.channel"
                  text-expr="'You said: ' + lastMessage" />
    </onentry>
  </state>
</agentml>
```

## Schema

Validation is provided by [`slack.xsd`](slack.xsd). Reference it in editors or CI via `https://xsd.agentml.dev/agentflare-ai/agentml-go/slack/slack.xsd`.

## Security

* **Bot Token**: Store your `SLACK_BOT_TOKEN` securely (environment variables, secret management).
* **Signing Secret**: When using the Router, use your Slack app's signing secret to verify requests.
* **Request Verification**: The Router automatically verifies Slack request signatures to prevent unauthorized access.

## Error Handling

The Slack namespace emits standard AgentML error events:

* **`error.execution`**: Invalid configuration, missing required attributes, or expression evaluation failures.
* **`error.communication`**: Network errors or Slack API failures.

Handle these in your state machine:

```xml
<transition event="error.communication" target="retry" />
<transition event="error.execution" target="error_state" />
```
