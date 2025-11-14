package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// JSONMap is a custom type for JSON columns (map[string]interface{})
type JSONMap map[string]interface{}

// Scan implements sql.Scanner interface
func (j *JSONMap) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("failed to unmarshal JSONMap value: %v", value)
	}
	result := make(map[string]interface{})
	err := json.Unmarshal(bytes, &result)
	*j = JSONMap(result)
	return err
}

// Value implements driver.Valuer interface
func (j JSONMap) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

// JSONStringArray is a custom type for JSON string arrays
type JSONStringArray []string

// Scan implements sql.Scanner interface
func (j *JSONStringArray) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("failed to unmarshal JSONStringArray value: %v", value)
	}
	result := make([]string, 0)
	err := json.Unmarshal(bytes, &result)
	*j = JSONStringArray(result)
	return err
}

// Value implements driver.Valuer interface
func (j JSONStringArray) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

// Helper functions for type conversions

// StringMapToJSONMap converts map[string]string to JSONMap (map[string]interface{})
func StringMapToJSONMap(m map[string]string) JSONMap {
	if m == nil {
		return nil
	}
	result := make(JSONMap, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

// JSONMapToStringMap converts JSONMap (map[string]interface{}) to map[string]string
func JSONMapToStringMap(m JSONMap) map[string]string {
	if m == nil {
		return nil
	}
	result := make(map[string]string, len(m))
	for k, v := range m {
		if str, ok := v.(string); ok {
			result[k] = str
		}
	}
	return result
}
