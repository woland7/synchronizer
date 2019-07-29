package utils

import (
	"bytes"
	"k8s.io/kubernetes/pkg/kubectl/scheme"
	jsonserializer "k8s.io/apimachinery/pkg/runtime/serializer/json"
	"github.com/jmoiron/jsonq"
	"strings"
	"encoding/json"
)

var version = "1.0.0"

// Decoder and encoder are two variables that will be used by all methods that access etcd
var Decoder = scheme.Codecs.UniversalDeserializer()
var Encoder = jsonserializer.NewSerializer(jsonserializer.DefaultMetaFactory, scheme.Scheme, scheme.Scheme, true)

// Support function. This function decodes json keys.
func GetJsonqQuery(keyvalue []byte) *jsonq.JsonQuery {
	obj, _, _ := Decoder.Decode(keyvalue, nil, nil)
	var b bytes.Buffer
	_ = Encoder.Encode(obj, &b)
	str := b.String()
	data := map[string]interface{}{}
	dec := json.NewDecoder(strings.NewReader(str))
	dec.Decode(&data)
	jq := jsonq.NewQuery(data)
	return jq
}

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
