package grpc_test

import (
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	controlv1 "github.com/WonderForgeLabs/gooey/grpc/gen/gooey/control/v1"
)

// TestTypedValueRoundTrip constructs one TypedValue per propKinds row
// and round-trips it through the wire format — the minimal proof that
// the generated package compiles, links, and speaks protobuf.
func TestTypedValueRoundTrip(t *testing.T) {
	cases := map[string]*controlv1.TypedValue{
		"string":   {Kind: &controlv1.TypedValue_StringValue{StringValue: "hi"}},
		"int":      {Kind: &controlv1.TypedValue_IntValue{IntValue: -42}},
		"bool":     {Kind: &controlv1.TypedValue_BoolValue{BoolValue: true}},
		"float":    {Kind: &controlv1.TypedValue_FloatValue{FloatValue: 1.5}},
		"duration": {Kind: &controlv1.TypedValue_DurationValue{DurationValue: durationpb.New(1500000000)}},
		"color":    {Kind: &controlv1.TypedValue_ColorValue{ColorValue: &controlv1.Color{Set: true, Red: 1, Green: 2, Blue: 3}}},
		"any":      {Kind: &controlv1.TypedValue_AnyJson{AnyJson: []byte(`{"x":1}`)}},
	}
	for name, tv := range cases {
		b, err := proto.Marshal(tv)
		if err != nil {
			t.Fatalf("%s: marshal: %v", name, err)
		}
		var got controlv1.TypedValue
		if err := proto.Unmarshal(b, &got); err != nil {
			t.Fatalf("%s: unmarshal: %v", name, err)
		}
		if !proto.Equal(tv, &got) {
			t.Errorf("%s: round-trip mismatch: sent %v got %v", name, tv, &got)
		}
	}
}

// TestColorSetDistinguishable pins the contract's unset-vs-black rule:
// a Color with Set=false must not compare equal to black-with-Set.
func TestColorSetDistinguishable(t *testing.T) {
	unset := &controlv1.Color{}
	black := &controlv1.Color{Set: true}
	if proto.Equal(unset, black) {
		t.Fatal("unset color and set-black color are indistinguishable on the wire")
	}
}

// TestServiceDescriptors asserts both services generated with their
// full RPC surface (the counts change only when the contract does).
func TestServiceDescriptors(t *testing.T) {
	if got := controlv1.ControlService_ServiceDesc.ServiceName; got != "gooey.control.v1.ControlService" {
		t.Errorf("ControlService name = %q", got)
	}
	if got := len(controlv1.ControlService_ServiceDesc.Methods); got != 12 {
		t.Errorf("ControlService has %d unary methods, want 12", got)
	}
	if got := controlv1.SessionService_ServiceDesc.ServiceName; got != "gooey.control.v1.SessionService" {
		t.Errorf("SessionService name = %q", got)
	}
	if got := len(controlv1.SessionService_ServiceDesc.Streams); got != 1 {
		t.Errorf("SessionService has %d streams, want 1", got)
	}
}
