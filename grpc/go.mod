module github.com/WonderForgeLabs/gooey/grpc

go 1.25.6

require (
	google.golang.org/grpc v1.83.2
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/rivo/uniseg v0.4.7 // indirect
	golang.org/x/image v0.44.0 // indirect
	golang.org/x/term v0.45.0 // indirect
)

require (
	github.com/WonderForgeLabs/gooey v0.0.0-20260822170725-f67f0f6cff61
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
)

replace github.com/WonderForgeLabs/gooey => ../
