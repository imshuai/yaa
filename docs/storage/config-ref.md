# Storage 配置参考

> 上级: [Storage 系统设计](README.md)
> 根结构: [完整配置参考](../config/reference.md)

---

## 1. 类型

```go
type StorageConfig struct {
    Type string `yaml:"type" json:"type"` // sqlite | memory
    Path string `yaml:"path" json:"path"`
}
```

`runtime.storage` 是唯一根 KV 配置位置。没有 `ttl_interval`、`wal`、`bucket` 或模块级 Storage override；strict decoder 对这些未知字段报错。

## 2. 默认值和校验

| 字段 | 默认值 | 校验 |
|------|--------|------|
| `type` | `sqlite` | `sqlite|memory` |
| `path` | `./data/yaa.db` | sqlite 时非空；memory 忽略 |

```go
func DefaultStorageConfig() StorageConfig {
    return StorageConfig{Type: "sqlite", Path: "./data/yaa.db"}
}
```

路径在环境变量展开后校验。SQLite 创建父目录失败、文件不可写、schema 版本未知或 migration 失败都会使 Runtime Not Ready。`type=memory` 明确不持久化 Session snapshot。

## 3. YAML

SQLite：

```yaml
runtime:
  storage:
    type: sqlite
    path: ./data/yaa.db
```

Memory（测试/临时运行）：

```yaml
runtime:
  storage:
    type: memory
```

## 4. 生效与安全

`runtime.storage.*` 全部需要重启；reload 候选包含这些路径时整批拒绝。配置/健康 API 可以返回 type 和脱敏后的相对 path，但不能返回文件内容或操作系统错误中的敏感绝对目录。

根 Storage 和 `memory.storage` 可以指向不同文件（默认如此），也可以显式使用同一 SQLite 文件；实现必须使用不同表名和 schema version namespace。

## 5. TTL 说明

TTL 是 `Storage.Set` 的调用参数，不是根配置。实现使用固定 60 秒 cleanup tick，并在读取时惰性隐藏过期 key。Session snapshot 写入不传 TTL；临时缓存 owner 若未来出现，必须在自身模块契约中声明具体 TTL。

---

*最后更新: 2026-07-22*
