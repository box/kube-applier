package v1alpha1

// ObjectReference is a reference to an object with a given name, in a given
// namespace. If Namespace is not specified, it implies the same namespace as
// the Waybill itself.
type ObjectReference struct {
	// Name of the resource being referred to.
	// +required
	Name string `json:"name"`

	// Namespace of the resource being referred to.
	// +optional
	Namespace string `json:"namespace,omitempty"`
}
