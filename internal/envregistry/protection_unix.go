//go:build darwin || linux

package envregistry

func protectValuesForStorage(values valuesFile) (valuesFile, error) {
	return cloneValues(values), nil
}

func unprotectValuesFromStorage(values valuesFile) (valuesFile, error) {
	if values.Protection != "" {
		return valuesFile{}, errUnsupportedProtection(values.Protection)
	}
	return cloneValues(values), nil
}
