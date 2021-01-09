// Package query provides helpers for building tiedot DB queries
package query

type Path []string

func Eq(v string, path Path) interface{} {
	return map[string]interface{}{
		"eq":    v,
		"in":    path.toIface(), // TODO: can we just use: path.([]string) ?
		"limit": 1,              // TODO: check duplicates by using >1 ?
	}
}

func (p Path) toIface() (result []interface{}) {
	result = make([]interface{}, len(p))
	for i := range p {
		result[i] = p[i]
	}
	return
}
