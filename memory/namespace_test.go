package memory

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/agentflare-ai/agentml-go"
	"github.com/agentflare-ai/go-xmldom"
)

type fakeDM struct{ store map[string]any }

func newFakeDM() *fakeDM { return &fakeDM{store: map[string]any{}} }

func (f *fakeDM) Initialize(ctx context.Context, dataElements []agentml.Data) error { return nil }
func (f *fakeDM) EvaluateValue(ctx context.Context, expression string) (any, error) {
	// Simple variable lookup or return literal as-is
	if v, ok := f.store[expression]; ok {
		return v, nil
	}
	// Strip single quotes for simple string literals
	if strings.HasPrefix(expression, "'") && strings.HasSuffix(expression, "'") && len(expression) >= 2 {
		return expression[1 : len(expression)-1], nil
	}
	return expression, nil
}
func (f *fakeDM) EvaluateCondition(ctx context.Context, expression string) (bool, error) { return false, nil }
func (f *fakeDM) EvaluateLocation(ctx context.Context, location string) (any, error) { return f.store[location], nil }
func (f *fakeDM) Assign(ctx context.Context, location string, value any) error { f.store[location] = value; return nil }
func (f *fakeDM) GetVariable(ctx context.Context, id string) (any, error) { return f.store[id], nil }
func (f *fakeDM) SetVariable(ctx context.Context, id string, value any) error { f.store[id] = value; return nil }
func (f *fakeDM) GetSystemVariable(ctx context.Context, name string) (any, error) { return nil, nil }
func (f *fakeDM) SetSystemVariable(ctx context.Context, name string, value any) error { return nil }
func (f *fakeDM) SetCurrentEvent(ctx context.Context, event any) error { return nil }
func (f *fakeDM) ExecuteScript(ctx context.Context, script string) error { return nil }
func (f *fakeDM) Clone(ctx context.Context) (agentml.DataModel, error) { return newFakeDM(), nil }
func (f *fakeDM) ValidateExpression(ctx context.Context, expression string, exprType agentml.ExpressionType) error {
	return nil
}

type fakeInterp struct{ dm *fakeDM }

func (fi *fakeInterp) Handle(ctx context.Context, event *agentml.Event) error { return nil }
func (fi *fakeInterp) Location(ctx context.Context) (string, error) { return "", nil }
func (fi *fakeInterp) Type() string { return "test" }
func (fi *fakeInterp) Shutdown(ctx context.Context) error { return nil }
func (fi *fakeInterp) SessionID() string { return "" }
func (fi *fakeInterp) Configuration() []string { return nil }
func (fi *fakeInterp) In(ctx context.Context, stateId string) bool { return false }
func (fi *fakeInterp) Raise(ctx context.Context, event *agentml.Event) {}
func (fi *fakeInterp) Send(ctx context.Context, event *agentml.Event) error { return nil }
func (fi *fakeInterp) Cancel(ctx context.Context, sendId string) error { return nil }
func (fi *fakeInterp) Log(ctx context.Context, label, message string) {}
func (fi *fakeInterp) Context() context.Context { return context.Background() }
func (fi *fakeInterp) Clock() agentml.Clock { return nil }
func (fi *fakeInterp) DataModel() agentml.DataModel { return fi.dm }
func (fi *fakeInterp) ExecuteElement(ctx context.Context, element xmldom.Element) error { return nil }
func (fi *fakeInterp) SendMessage(ctx context.Context, data agentml.SendData) error { return nil }
func (fi *fakeInterp) ScheduleMessage(ctx context.Context, data agentml.SendData) (string, error) { return "", nil }
func (fi *fakeInterp) InvokedSessions() map[string]agentml.Interpreter { return nil }
func (fi *fakeInterp) Tracer() agentml.Tracer { return nil }
func (fi *fakeInterp) Snapshot(ctx context.Context, maybeConfig ...agentml.SnapshotConfig) (xmldom.Document, error) {
	return nil, nil
}

func withTimeout(tb testing.TB) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}

func TestSingleDBWithDSNExpr(t *testing.T) {
	ctx, cancel := withTimeout(t)
	defer cancel()
	// Prepare document with memory:db and nested put/get
	xml := `<?xml version="1.0"?>
<agentml xmlns="github.com/agentflare-ai/agentml" xmlns:memory="github.com/agentflare-ai/agentml-go/memory">
  <memory:db id="foo" dsnexpr="dsn">
    <memory:put key="k" value="v"/>
    <memory:get key="k" location="out"/>
  </memory:db>
</agentml>`
	dec := xmldom.NewDecoder(strings.NewReader(xml))
	doc, err := dec.Decode()
	if err != nil { t.Fatalf("decode: %v", err) }
	dm := newFakeDM()
	dm.store["dsn"] = "file:mem_ns_test1.db?_foreign_keys=on"
	it := &fakeInterp{dm: dm}
	ns, err := Loader()(ctx, it, doc)
	if err != nil { t.Fatalf("loader: %v", err) }
	root := doc.DocumentElement()
puts := root.GetElementsByTagNameNS(xmldom.DOMString(MemoryNamespaceURI), "put")
gets := root.GetElementsByTagNameNS(xmldom.DOMString(MemoryNamespaceURI), "get")
	if puts.Length() == 0 || gets.Length() == 0 { t.Fatalf("expected put/get elements") }
	putEl, _ := puts.Item(0).(xmldom.Element)
	getEl, _ := gets.Item(0).(xmldom.Element)
	if ok, err := ns.Handle(ctx, putEl); !ok || err != nil { t.Fatalf("put handle: %v", err) }
	if ok, err := ns.Handle(ctx, getEl); !ok || err != nil { t.Fatalf("get handle: %v", err) }
	if v, _ := dm.GetVariable(ctx, "out"); v != "v" { t.Fatalf("got %v want 'v'", v) }
	_ = os.Remove("mem_ns_test1.db")
}

func TestAmbiguousWithoutDB(t *testing.T) {
	ctx, cancel := withTimeout(t)
	defer cancel()
	xml := `<?xml version="1.0"?>
<agentml xmlns="github.com/agentflare-ai/agentml" xmlns:memory="github.com/agentflare-ai/agentml-go/memory">
  <memory:db id="a" dsn=":memory:?_foreign_keys=on"/>
  <memory:db id="b" dsn=":memory:?_foreign_keys=on"/>
  <memory:put key="k" value="v"/>
</agentml>`
	doc, _ := xmldom.NewDecoder(strings.NewReader(xml)).Decode()
	dm := newFakeDM()
	it := &fakeInterp{dm: dm}
	ns, err := Loader()(ctx, it, doc)
	if err != nil { t.Fatalf("loader: %v", err) }
puts := doc.DocumentElement().GetElementsByTagNameNS(xmldom.DOMString(MemoryNamespaceURI), "put")
	el, _ := puts.Item(0).(xmldom.Element)
	if ok, err := ns.Handle(ctx, el); !ok || err == nil {
		t.Fatalf("expected ambiguous db error, got ok=%v err=%v", ok, err)
	}
}

func TestImplicitDefaultNoDb(t *testing.T) {
	ctx, cancel := withTimeout(t)
	defer cancel()
	xml := `<?xml version="1.0"?>
<agentml xmlns="github.com/agentflare-ai/agentml" xmlns:memory="github.com/agentflare-ai/agentml-go/memory">
  <memory:put key="k" value="v"/>
  <memory:get key="k" location="out"/>
</agentml>`
	doc, _ := xmldom.NewDecoder(strings.NewReader(xml)).Decode()
	dm := newFakeDM()
	it := &fakeInterp{dm: dm}
	ns, err := Loader()(ctx, it, doc)
	if err != nil { t.Fatalf("loader: %v", err) }
els := doc.DocumentElement().GetElementsByTagNameNS(xmldom.DOMString(MemoryNamespaceURI), "put")
	putEl, _ := els.Item(0).(xmldom.Element)
els = doc.DocumentElement().GetElementsByTagNameNS(xmldom.DOMString(MemoryNamespaceURI), "get")
	getEl, _ := els.Item(0).(xmldom.Element)
	if ok, err := ns.Handle(ctx, putEl); !ok || err != nil { t.Fatalf("put: %v", err) }
	if ok, err := ns.Handle(ctx, getEl); !ok || err != nil { t.Fatalf("get: %v", err) }
	if v, _ := dm.GetVariable(ctx, "out"); v != "v" { t.Fatalf("got %v want 'v'", v) }
}

func TestPerDbIsolation(t *testing.T) {
	ctx, cancel := withTimeout(t)
	defer cancel()
	xml := `<?xml version="1.0"?>
<agentml xmlns="github.com/agentflare-ai/agentml" xmlns:memory="github.com/agentflare-ai/agentml-go/memory">
  <memory:db id="foo" dsn=":memory:?_foreign_keys=on"/>
  <memory:db id="bar" dsn=":memory:?_foreign_keys=on"/>
  <memory:put db="foo" key="k" value="v1"/>
  <memory:put db="bar" key="k" value="v2"/>
  <memory:get db="foo" key="k" location="out1"/>
  <memory:get db="bar" key="k" location="out2"/>
</agentml>`
	doc, _ := xmldom.NewDecoder(strings.NewReader(xml)).Decode()
	dm := newFakeDM()
	it := &fakeInterp{dm: dm}
	ns, err := Loader()(ctx, it, doc)
	if err != nil { t.Fatalf("loader: %v", err) }
	root := doc.DocumentElement()
for _, name := range []string{"put", "put", "get", "get"} {
els := root.GetElementsByTagNameNS(xmldom.DOMString(MemoryNamespaceURI), xmldom.DOMString(name))
		// process all occurrences in document order
		for i := uint(0); i < els.Length(); i++ {
			el, _ := els.Item(i).(xmldom.Element)
			if ok, err := ns.Handle(ctx, el); !ok || err != nil { t.Fatalf("%s: %v", name, err) }
		}
	}
	if dm.store["out1"] != "v1" || dm.store["out2"] != "v2" {
		t.Fatalf("isolation failed: out1=%v out2=%v", dm.store["out1"], dm.store["out2"])
	}
}

func TestTransactionsPerDB(t *testing.T) {
	ctx, cancel := withTimeout(t)
	defer cancel()
	xml := `<?xml version="1.0"?>
<agentml xmlns="github.com/agentflare-ai/agentml" xmlns:memory="github.com/agentflare-ai/agentml-go/memory">
  <memory:db id="foo" dsn=":memory:?_foreign_keys=on"/>
  <memory:begin db="foo"/>
  <memory:put db="foo" key="tk" value="tv"/>
  <memory:commit db="foo"/>
  <memory:get db="foo" key="tk" location="out"/>
</agentml>`
	doc, _ := xmldom.NewDecoder(strings.NewReader(xml)).Decode()
	dm := newFakeDM()
	it := &fakeInterp{dm: dm}
	ns, err := Loader()(ctx, it, doc)
	if err != nil { t.Fatalf("loader: %v", err) }
	root := doc.DocumentElement()
	order := []string{"begin", "put", "commit", "get"}
	for _, name := range order {
els := root.GetElementsByTagNameNS(xmldom.DOMString(MemoryNamespaceURI), xmldom.DOMString(name))
		for i := uint(0); i < els.Length(); i++ {
			el, _ := els.Item(i).(xmldom.Element)
			if ok, err := ns.Handle(ctx, el); !ok || err != nil { t.Fatalf("%s: %v", name, err) }
		}
	}
	if dm.store["out"] != "tv" { t.Fatalf("got %v want 'tv'", dm.store["out"]) }
}

func TestCloseReopen(t *testing.T) {
	ctx, cancel := withTimeout(t)
	defer cancel()
	dbFile := "mem_ns_test_close.db"
	dsn := "file:" + dbFile + "?_foreign_keys=on"
	xml := `<?xml version="1.0"?>
<agentml xmlns="github.com/agentflare-ai/agentml" xmlns:memory="github.com/agentflare-ai/agentml-go/memory">
  <memory:db id="foo" dsnexpr="dsn"/>
  <memory:put db="foo" key="k" value="v"/>
  <memory:close db="foo"/>
  <memory:get db="foo" key="k" location="out"/>
</agentml>`
	doc, _ := xmldom.NewDecoder(strings.NewReader(xml)).Decode()
	dm := newFakeDM(); dm.store["dsn"] = dsn
	it := &fakeInterp{dm: dm}
	ns, err := Loader()(ctx, it, doc)
	if err != nil { t.Fatalf("loader: %v", err) }
	root := doc.DocumentElement()
	for _, name := range []string{"put", "close", "get"} {
els := root.GetElementsByTagNameNS(xmldom.DOMString(MemoryNamespaceURI), xmldom.DOMString(name))
		for i := uint(0); i < els.Length(); i++ {
			el, _ := els.Item(i).(xmldom.Element)
			if ok, err := ns.Handle(ctx, el); !ok || err != nil { t.Fatalf("%s: %v", name, err) }
		}
	}
	if dm.store["out"] != "v" { t.Fatalf("got %v want 'v'", dm.store["out"]) }
	_ = os.Remove(dbFile)
}