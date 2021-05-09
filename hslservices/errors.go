package hsldatabridge

// MQTTValidationError -
type MQTTValidationError struct {
	Message string
}

func (e MQTTValidationError) Error() string {
	return e.Message
}
