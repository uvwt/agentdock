package core

import "encoding/json"

// Remarshal 通过 JSON 契约在动态边界值和明确结构之间转换。
func Remarshal(input, output any) error {
	data, err := json.Marshal(input)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, output)
}
