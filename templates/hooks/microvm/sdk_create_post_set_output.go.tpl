	// RunMicrovm is create-only and idempotency requires retries to carry the
	// exact parameters used by the first request. Do not persist service defaults
	// or normalized output fields back into the desired spec.
	ko.Spec = *desired.ko.Spec.DeepCopy()
