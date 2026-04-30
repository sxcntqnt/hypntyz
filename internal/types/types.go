package types

type Viewport struct {
	MinLat float64
	MinLon float64
	MaxLat float64
	MaxLon float64
}

type ClientState struct {
	ID       string
	Viewport Viewport
	FocusLat float64
	FocusLon float64
}
