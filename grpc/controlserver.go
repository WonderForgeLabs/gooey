package grpc

import (
	"context"

	"github.com/WonderForgeLabs/gooey/control"

	controlv1 "github.com/WonderForgeLabs/gooey/grpc/gen/gooey/control/v1"
)

// controlServer adapts ControlService onto the shared in-process
// service. Every method is the same three lines with different types:
// convert the request, cross the bridge (UI goroutine + settle
// barrier), convert the result — the "thin adapter" shape issue #112
// prescribes for every transport.
type controlServer struct {
	controlv1.UnimplementedControlServiceServer
	s *Server
}

// do crosses the bridge and maps failures onto the contract's status
// codes. fn runs on the UI goroutine; by the time do returns, the frame
// fn's Sets asked for has been composed.
func (c *controlServer) do(fn func() error) error {
	return statusOf(c.s.ui.Do(fn))
}

func (c *controlServer) SnapshotTree(_ context.Context, req *controlv1.SnapshotTreeRequest) (*controlv1.SnapshotTreeResponse, error) {
	var root *control.Node
	if err := c.do(func() (err error) {
		root, err = c.s.svc.Tree(int(req.GetDepth()))
		return
	}); err != nil {
		return nil, err
	}
	return &controlv1.SnapshotTreeResponse{Root: nodeToProto(root)}, nil
}

func (c *controlServer) ScreenText(_ context.Context, req *controlv1.ScreenTextRequest) (*controlv1.ScreenTextResponse, error) {
	var text string
	if err := c.do(func() (err error) {
		text, err = c.s.svc.Screen(req.GetStyled())
		return
	}); err != nil {
		return nil, err
	}
	return &controlv1.ScreenTextResponse{Text: text}, nil
}

func (c *controlServer) ListValues(_ context.Context, _ *controlv1.ListValuesRequest) (*controlv1.ListValuesResponse, error) {
	var entries []control.ValueEntry
	var named []string
	if err := c.do(func() (err error) {
		entries, named, err = c.s.svc.Values()
		return
	}); err != nil {
		return nil, err
	}
	res := &controlv1.ListValuesResponse{Named: named}
	for _, e := range entries {
		res.Values = append(res.Values, entryToProto(e))
	}
	return res, nil
}

func (c *controlServer) GetProperty(_ context.Context, req *controlv1.GetPropertyRequest) (*controlv1.GetPropertyResponse, error) {
	var entry control.ValueEntry
	if err := c.do(func() (err error) {
		entry, err = c.s.svc.Value(req.GetName())
		return
	}); err != nil {
		return nil, err
	}
	return &controlv1.GetPropertyResponse{Value: entryToProto(entry)}, nil
}

func (c *controlServer) SetProperty(_ context.Context, req *controlv1.SetPropertyRequest) (*controlv1.SetPropertyResponse, error) {
	v, err := valueFromProto(req.GetValue())
	if err != nil {
		return nil, err
	}
	if err := c.do(func() error {
		return c.s.svc.Set(req.GetName(), v)
	}); err != nil {
		return nil, err
	}
	return &controlv1.SetPropertyResponse{}, nil
}

func (c *controlServer) InvokeCommand(_ context.Context, req *controlv1.InvokeCommandRequest) (*controlv1.InvokeCommandResponse, error) {
	if err := c.do(func() error {
		return c.s.svc.Invoke(req.GetName())
	}); err != nil {
		return nil, err
	}
	return &controlv1.InvokeCommandResponse{}, nil
}

func (c *controlServer) SendKeys(_ context.Context, req *controlv1.SendKeysRequest) (*controlv1.SendKeysResponse, error) {
	var consumed []bool
	if err := c.do(func() (err error) {
		consumed, err = c.s.svc.SendKeys(req.GetText(), req.GetGestures())
		return
	}); err != nil {
		return nil, err
	}
	return &controlv1.SendKeysResponse{Sent: int32(len(consumed)), Consumed: consumed}, nil
}

func (c *controlServer) SendPointer(_ context.Context, req *controlv1.SendPointerRequest) (*controlv1.SendPointerResponse, error) {
	p, err := pointerFromProto(req.GetEvent())
	if err != nil {
		return nil, err
	}
	var consumed bool
	if err := c.do(func() (err error) {
		consumed, err = c.s.svc.SendPointer(p)
		return
	}); err != nil {
		return nil, err
	}
	return &controlv1.SendPointerResponse{Consumed: consumed}, nil
}

func (c *controlServer) SetFocus(_ context.Context, req *controlv1.SetFocusRequest) (*controlv1.SetFocusResponse, error) {
	if err := c.do(func() error {
		return c.s.svc.Focus(req.GetName())
	}); err != nil {
		return nil, err
	}
	return &controlv1.SetFocusResponse{}, nil
}

func (c *controlServer) SwapMarkup(_ context.Context, req *controlv1.SwapMarkupRequest) (*controlv1.SwapMarkupResponse, error) {
	regs, err := registrationsFromProto(req.GetRegister())
	if err != nil {
		return nil, err
	}
	var named []string
	if err := c.do(func() (err error) {
		named, err = c.s.svc.SwapMarkup(req.GetSource(), regs)
		return
	}); err != nil {
		return nil, err
	}
	return &controlv1.SwapMarkupResponse{Named: named}, nil
}

func (c *controlServer) RegisterProperties(_ context.Context, req *controlv1.RegisterPropertiesRequest) (*controlv1.RegisterPropertiesResponse, error) {
	regs, err := registrationsFromProto(req.GetProperties())
	if err != nil {
		return nil, err
	}
	if err := c.do(func() error {
		return c.s.svc.Register(regs)
	}); err != nil {
		return nil, err
	}
	return &controlv1.RegisterPropertiesResponse{}, nil
}

func (c *controlServer) UnregisterNames(_ context.Context, req *controlv1.UnregisterNamesRequest) (*controlv1.UnregisterNamesResponse, error) {
	if err := c.do(func() error {
		return c.s.svc.Unregister(req.GetNames())
	}); err != nil {
		return nil, err
	}
	return &controlv1.UnregisterNamesResponse{}, nil
}

func (c *controlServer) GetDeclaredSchema(_ context.Context, req *controlv1.GetDeclaredSchemaRequest) (*controlv1.GetDeclaredSchemaResponse, error) {
	var schema *control.Schema
	if err := c.do(func() (err error) {
		schema, err = c.s.svc.DeclaredSchema(req.GetSource())
		return
	}); err != nil {
		return nil, err
	}
	return &controlv1.GetDeclaredSchemaResponse{Schema: schemaToProto(schema)}, nil
}

func (c *controlServer) PatchMarkup(_ context.Context, req *controlv1.PatchMarkupRequest) (*controlv1.PatchMarkupResponse, error) {
	var named []string
	if err := c.do(func() (err error) {
		named, err = c.s.svc.PatchMarkup(req.GetName(), req.GetSource())
		return
	}); err != nil {
		return nil, err
	}
	return &controlv1.PatchMarkupResponse{Named: named}, nil
}

func (c *controlServer) ListStyles(_ context.Context, _ *controlv1.ListStylesRequest) (*controlv1.ListStylesResponse, error) {
	var styles []control.StyleEntry
	if err := c.do(func() (err error) {
		styles, err = c.s.svc.Styles()
		return
	}); err != nil {
		return nil, err
	}
	res := &controlv1.ListStylesResponse{}
	for _, st := range styles {
		res.Styles = append(res.Styles, styleToProto(st))
	}
	return res, nil
}

// ValidateMarkup is the one RPC where a bad document is NOT a status
// error: the caller asked whether the markup is valid, and valid=false
// is the answer. Only a missing context (or a dead run loop) is a
// status.
func (c *controlServer) ValidateMarkup(_ context.Context, req *controlv1.ValidateMarkupRequest) (*controlv1.ValidateMarkupResponse, error) {
	res := &controlv1.ValidateMarkupResponse{}
	if err := c.do(func() error {
		valid, loadErr, named, err := c.s.svc.Validate(req.GetSource())
		if err != nil {
			return err
		}
		res.Valid, res.Error, res.Named = valid, loadErr, named
		return nil
	}); err != nil {
		return nil, err
	}
	return res, nil
}
