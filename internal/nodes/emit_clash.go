package nodes

// EmitClash строит прокси Clash из узла.
func EmitClash(n Node) (map[string]any, error) { return nil, NewError("unsupported_type") }

// ClashProxy — прокси с именем.
func ClashProxy(n Node, name string) (map[string]any, error) {
	return nil, NewError("unsupported_type")
}

// ClashFile сериализует прокси в документ proxies.
func ClashFile(proxies []map[string]any) ([]byte, error) { return nil, NewError("unsupported_type") }
