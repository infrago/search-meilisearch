# search-meilisearch

`search-meilisearch` 是 `search` 模块的 `meilisearch` 驱动。

## 安装

```bash
go get github.com/infrago/search@latest
go get github.com/infrago/search-meilisearch@latest
```

## 接入

```go
import (
    _ "github.com/infrago/search"
    _ "github.com/infrago/search-meilisearch"
    "github.com/infrago/infra"
)

func main() {
    infra.Run()
}
```

## 配置示例

```toml
[search]
driver = "meilisearch"
```

## 公开 API（摘自源码）

- `func (d *meiliDriver) Connect(inst *search.Instance) (search.Connection, error)`
- `func (c *meiliConnection) Open() error  { return nil }`
- `func (c *meiliConnection) Close() error { return nil }`
- `func (c *meiliConnection) Capabilities() search.Capabilities`
- `func (c *meiliConnection) SyncIndex(name string, index search.Index) error`
- `func (c *meiliConnection) Clear(name string) error`
- `func (c *meiliConnection) Upsert(index string, rows []Map) error`
- `func (c *meiliConnection) Delete(index string, ids []string) error`
- `func (c *meiliConnection) Search(index string, query search.Query) (search.Result, error)`
- `func (c *meiliConnection) Count(index string, query search.Query) (int64, error)`

## 排错

- driver 未生效：确认模块段 `driver` 值与驱动名一致
- 连接失败：检查 endpoint/host/port/鉴权配置
