package clip

// RenderedFrame is what the export pipeline moves between stages. Alpha is the
// optional Gray8 sidecar; nil means fully opaque.
type RenderedFrame struct {
	RGB   *Frame
	Alpha *Frame
	Index int
}
