package nodes

// EmitXray строит outbound Xray из узла.
func EmitXray(n Node) (map[string]any, error) { return nil, NewError("unsupported_type") }

// XrayOutbound — outbound с тегом и меткой.
func XrayOutbound(n Node, tag string) (map[string]any, error) {
	return nil, NewError("unsupported_type")
}

// XrayFile сериализует outbounds в файл конфигурации.
func XrayFile(outbounds []map[string]any) ([]byte, error) { return nil, NewError("unsupported_type") }
