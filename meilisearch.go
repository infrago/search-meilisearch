package search_meilisearch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/infrago/infra"
	. "github.com/infrago/base"
	"github.com/infrago/search"
)

type meiliDriver struct{}

type meiliConnection struct {
	server string
	key    string
	prefix string
	client *http.Client
}

func init() {
	infra.Register("meilisearch", &meiliDriver{})
	infra.Register("meili", &meiliDriver{})
}

func (d *meiliDriver) Connect(inst *search.Instance) (search.Connection, error) {
	server := pickString(inst.Config.Setting, "server", "host", "url")
	if server == "" {
		server = "http://127.0.0.1:7700"
	}
	key := pickString(inst.Config.Setting, "api_key", "apikey", "key")
	timeout := 5 * time.Second
	if inst.Config.Timeout > 0 {
		timeout = inst.Config.Timeout
	}
	if v, ok := inst.Config.Setting["timeout"].(string); ok {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			timeout = d
		}
	}
	prefix := inst.Config.Prefix
	if prefix == "" {
		prefix = pickString(inst.Config.Setting, "prefix", "index_prefix")
	}
	return &meiliConnection{server: strings.TrimRight(server, "/"), key: key, prefix: prefix, client: &http.Client{Timeout: timeout}}, nil
}

func (c *meiliConnection) Open() error  { return nil }
func (c *meiliConnection) Close() error { return nil }
func (c *meiliConnection) Capabilities() search.Capabilities {
	return search.Capabilities{
		SyncIndex: true,
		Clear:     true,
		Upsert:    true,
		Delete:    true,
		Search:    true,
		Count:     true,
		Suggest:   false,
		Sort:      true,
		Facets:    true,
		Highlight: true,
		FilterOps: []string{OpEq, OpNe, OpIn, OpNin, OpGt, OpGte, OpLt, OpLte, OpRange},
	}
}

func (c *meiliConnection) SyncIndex(name string, index search.Index) error {
	uid := c.indexName(name)
	payload := Map{"uid": uid}
	pk := index.Primary
	if pk == "" {
		pk = "id"
	}
	payload["primaryKey"] = pk
	_, err := c.request(http.MethodPost, "/indexes", payload)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "already exists") {
		err = nil
	}
	if err != nil {
		return err
	}

	settings := buildSettings(index)
	if len(settings) > 0 {
		_, err = c.request(http.MethodPatch, "/indexes/"+url.PathEscape(uid)+"/settings", settings)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *meiliConnection) Clear(name string) error {
	uid := url.PathEscape(c.indexName(name))
	_, err := c.request(http.MethodPost, "/indexes/"+uid+"/documents/delete", Map{})
	if err == nil {
		return nil
	}
	_, err2 := c.request(http.MethodDelete, "/indexes/"+uid+"/documents", nil)
	if err2 == nil {
		return nil
	}
	return err
}

func (c *meiliConnection) Upsert(index string, rows []Map) error {
	uid := url.PathEscape(c.indexName(index))
	body := make([]Map, 0, len(rows))
	for _, row := range rows {
		if row == nil {
			continue
		}
		id := fmt.Sprintf("%v", row["id"])
		if id == "" || id == "<nil>" {
			continue
		}
		payload := cloneMap(row)
		payload["id"] = id
		body = append(body, payload)
	}
	if len(body) == 0 {
		return nil
	}
	_, err := c.request(http.MethodPost, "/indexes/"+uid+"/documents?primaryKey=id", body)
	return err
}

func (c *meiliConnection) Delete(index string, ids []string) error {
	uid := url.PathEscape(c.indexName(index))
	if len(ids) == 1 {
		_, err := c.request(http.MethodDelete, "/indexes/"+uid+"/documents/"+url.PathEscape(ids[0]), nil)
		return err
	}
	_, err := c.request(http.MethodPost, "/indexes/"+uid+"/documents/delete-batch", ids)
	return err
}

func (c *meiliConnection) Search(index string, query search.Query) (search.Result, error) {
	uid := url.PathEscape(c.indexName(index))
	payload := Map{"q": query.Keyword, "offset": query.Offset, "limit": query.Limit}
	if len(query.Fields) > 0 {
		payload["attributesToRetrieve"] = query.Fields
	}
	if len(query.Facets) > 0 {
		payload["facets"] = query.Facets
	}
	if len(query.Sorts) > 0 {
		sorts := make([]string, 0, len(query.Sorts))
		for _, s := range query.Sorts {
			order := "asc"
			if s.Desc {
				order = "desc"
			}
			sorts = append(sorts, s.Field+":"+order)
		}
		payload["sort"] = sorts
	}
	if len(query.Highlight) > 0 {
		payload["attributesToHighlight"] = query.Highlight
	}
	if expr := meiliFilterExpr(query.Filters); expr != "" {
		payload["filter"] = expr
	}

	respBytes, err := c.request(http.MethodPost, "/indexes/"+uid+"/search", payload)
	if err != nil {
		return search.Result{}, err
	}

	var resp struct {
		Hits               []Map                       `json:"hits"`
		EstimatedTotalHits int64                       `json:"estimatedTotalHits"`
		ProcessingTimeMs   int64                       `json:"processingTimeMs"`
		FacetDistribution  map[string]map[string]int64 `json:"facetDistribution"`
	}
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return search.Result{}, err
	}

	hits := make([]search.Hit, 0, len(resp.Hits))
	for _, row := range resp.Hits {
		row = mergeMeiliFormatted(row)
		id := fmt.Sprintf("%v", row["id"])
		if id == "<nil>" {
			id = ""
		}
		hits = append(hits, search.Hit{ID: id, Score: 1, Payload: row})
	}

	facets := map[string][]search.Facet{}
	for field, vals := range resp.FacetDistribution {
		arr := make([]search.Facet, 0, len(vals))
		for val, count := range vals {
			arr = append(arr, search.Facet{Field: field, Value: val, Count: count})
		}
		facets[field] = arr
	}

	return search.Result{Total: resp.EstimatedTotalHits, Took: resp.ProcessingTimeMs, Hits: hits, Facets: facets}, nil
}

func (c *meiliConnection) Count(index string, query search.Query) (int64, error) {
	query.Offset = 0
	query.Limit = 1
	res, err := c.Search(index, query)
	if err != nil {
		return 0, err
	}
	return res.Total, nil
}

func (c *meiliConnection) request(method, path string, body Any) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		bts, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(bts)
	}
	req, err := http.NewRequest(method, c.server+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	bts, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("meilisearch %s %s failed: %s", method, path, strings.TrimSpace(string(bts)))
	}
	return bts, nil
}

func (c *meiliConnection) indexName(name string) string {
	if c.prefix == "" {
		return name
	}
	return c.prefix + "_" + name
}

func pickString(m Map, keys ...string) string {
	if m == nil {
		return ""
	}
	for _, key := range keys {
		if v, ok := m[key].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func meiliFilterExpr(filters []search.Filter) string {
	parts := make([]string, 0)
	for _, f := range filters {
		field := strings.TrimSpace(f.Field)
		if field == "" {
			continue
		}
		op := strings.ToLower(strings.TrimSpace(f.Op))
		if op == "" {
			op = search.FilterEq
		}
		switch op {
		case search.FilterEq, "=":
			parts = append(parts, fmt.Sprintf("%s = %s", field, meiliValue(f.Value)))
		case search.FilterNe, "!=":
			parts = append(parts, fmt.Sprintf("%s != %s", field, meiliValue(f.Value)))
		case search.FilterGt, ">", search.FilterGte, ">=", search.FilterLt, "<", search.FilterLte, "<=":
			parts = append(parts, fmt.Sprintf("%s %s %s", field, opSymbol(op), meiliValue(f.Value)))
		case search.FilterIn:
			vals := f.Values
			if len(vals) == 0 && f.Value != nil {
				vals = []Any{f.Value}
			}
			arr := make([]string, 0, len(vals))
			for _, one := range vals {
				arr = append(arr, meiliValue(one))
			}
			if len(arr) > 0 {
				parts = append(parts, fmt.Sprintf("%s IN [%s]", field, strings.Join(arr, ",")))
			}
		case search.FilterRange:
			if f.Min != nil {
				parts = append(parts, fmt.Sprintf("%s >= %s", field, meiliValue(f.Min)))
			}
			if f.Max != nil {
				parts = append(parts, fmt.Sprintf("%s <= %s", field, meiliValue(f.Max)))
			}
		}
	}
	return strings.Join(parts, " AND ")
}

func buildSettings(index search.Index) Map {
	out := Map{}
	if index.Setting != nil {
		out = cloneMap(index.Setting)
	}
	if len(index.Attributes) == 0 {
		return out
	}
	searchable := make([]string, 0, len(index.Attributes))
	filterable := make([]string, 0, len(index.Attributes))
	sortable := make([]string, 0, len(index.Attributes))
	for field, v := range index.Attributes {
		if strings.TrimSpace(field) == "" {
			continue
		}
		searchable = append(searchable, field)
		t := strings.ToLower(strings.TrimSpace(v.Type))
		if isExactType(t) {
			filterable = append(filterable, field)
		}
		if isSortableType(t) {
			sortable = append(sortable, field)
		}
	}
	if len(searchable) > 0 {
		out["searchableAttributes"] = searchable
	}
	if len(filterable) > 0 {
		out["filterableAttributes"] = filterable
	}
	if len(sortable) > 0 {
		out["sortableAttributes"] = sortable
	}
	return out
}

func isExactType(t string) bool {
	switch t {
	case "bool", "boolean", "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "float", "float32", "float64", "number", "decimal", "timestamp", "datetime", "date", "time":
		return true
	default:
		return false
	}
}

func isSortableType(t string) bool {
	switch t {
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "float", "float32", "float64", "number", "decimal", "timestamp", "datetime", "date", "time", "string":
		return true
	default:
		return false
	}
}

func meiliValue(v Any) string {
	switch vv := v.(type) {
	case int, int64, float64, float32:
		return fmt.Sprintf("%v", vv)
	case bool:
		if vv {
			return "true"
		}
		return "false"
	case string:
		if _, err := strconv.ParseFloat(vv, 64); err == nil {
			return vv
		}
		return `"` + strings.ReplaceAll(vv, `"`, `\"`) + `"`
	default:
		s := fmt.Sprintf("%v", vv)
		if _, err := strconv.ParseFloat(s, 64); err == nil {
			return s
		}
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
}

func opSymbol(op string) string {
	switch op {
	case search.FilterGt:
		return ">"
	case search.FilterGte:
		return ">="
	case search.FilterLt:
		return "<"
	case search.FilterLte:
		return "<="
	default:
		return op
	}
}

func cloneMap(src Map) Map {
	if src == nil {
		return Map{}
	}
	out := Map{}
	for k, v := range src {
		out[k] = v
	}
	return out
}

func mergeMeiliFormatted(row Map) Map {
	if row == nil {
		return Map{}
	}
	formatted, ok := row["_formatted"].(map[string]any)
	if ok {
		for key, value := range formatted {
			row[key] = value
		}
	}
	if formattedMap, ok := row["_formatted"].(Map); ok {
		for key, value := range formattedMap {
			row[key] = value
		}
	}
	delete(row, "_formatted")
	delete(row, "_matchesPosition")
	return row
}
