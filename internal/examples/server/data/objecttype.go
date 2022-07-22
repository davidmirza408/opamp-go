package data

type ObjectType string

const (
	// Type of Opamp Solution
	Deployment     ObjectType = "deployment"
	OtelOperator   ObjectType = "otelOperator"
	OtelStandalone ObjectType = "otelStandalone"
)
