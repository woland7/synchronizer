package utils

import "bytes"

var version = "1.0.0"

func GenerateKey(prefix string, namespace string, key string, options ...string) string {
	var b bytes.Buffer
	b.WriteString(prefix)
	b.WriteString(namespace)
	b.WriteString("/")
	b.WriteString(key)
	for _, s := range options {
		b.WriteString(s)
	}
	return b.String()
}
