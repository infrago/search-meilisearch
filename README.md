# search-meilisearch

`search` 的 Meilisearch 驱动。

驱动名：`meilisearch`（别名：`meili`）

## 使用

```go
import _ "github.com/bamgoo/search-meilisearch"
```

```toml
[search]
driver = "meilisearch"
prefix = "demo"

[search.setting]
server = "http://127.0.0.1:7700"
api_key = ""
```

## 配置项

- `server`：Meilisearch 地址
- `api_key`：API Key（可选）
- `prefix`：索引前缀（可选）
- `timeout`：HTTP 超时（例如 `3s`）

## 映射说明

1. `Search` 统一 DSL 会映射到 Meili 的 `q/filter/sort/facets/highlight`。
2. `filters` 会转换为 Meili filter expression。
3. `Suggest` 当前基于 `Search` 结果返回 ID 列表。
