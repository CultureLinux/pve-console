package proxmox

import (
	"encoding/json"
	"fmt"
	"strconv"
)

type Bool bool

func (b *Bool) UnmarshalJSON(data []byte) error {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	switch value := raw.(type) {
	case bool:
		*b = Bool(value)
		return nil
	case float64:
		*b = Bool(value != 0)
		return nil
	case string:
		parsed, err := strconv.ParseBool(value)
		if err == nil {
			*b = Bool(parsed)
			return nil
		}
		number, err := strconv.Atoi(value)
		if err == nil {
			*b = Bool(number != 0)
			return nil
		}
	}

	return fmt.Errorf("invalid boolean value %q", string(data))
}
