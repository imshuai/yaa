# Storage 实现检查清单

> 依据 [Storage 系统设计](README.md)。

---

## 接口

- [ ] Storage 完整实现 Get/Set/Delete/Has/Keys/Close。
- [ ] ErrNotFound/Closed/InvalidKey/InvalidTTL/InvalidPath/ValueTooLarge 可用 errors.Is 判断。
- [ ] key/value/TTL 固定上限在 sqlite/memory 共同 wrapper 校验。
- [ ] Set/Get 深拷贝 bytes；Delete 缺失和 Close 重复调用幂等。
- [ ] Keys 隐藏过期值并按 key 稳定升序。

## SQLite

- [ ] 使用 `modernc.org/sqlite`，无 CGO；root 表名不与 Memory 冲突。
- [ ] schema version、migration、WAL、busy timeout 和单连接初始化失败均阻止 Ready。
- [ ] Set 使用原子 upsert；Get/Has/Keys 在 SQL 中过滤 expiry。
- [ ] cleanup 按 expiry/key 排序，每批最多 1000，支持关闭等待。
- [ ] Close 后所有方法返回 ErrClosed；online backup/integrity check 有集成测试。

## Memory 后端

- [ ] 使用 injected Clock，不靠 sleep 测 TTL。
- [ ] 唯一 60 秒 worker 做 batch 1000 清理；惰性读取仍隐藏过期值。
- [ ] Close 标记 closed、停止并等待 worker；重复 Close 幂等。
- [ ] map 内 value 不暴露给调用方；Keys 与 SQLite 排序一致。
- [ ] health 明确 `durable=false`。

## 集成

- [ ] `runtime.storage.type` 只接受 `sqlite|memory`，Config 只有 Type/Path。
- [ ] Root Storage 所有者是 Runtime；Session Manager 不 Close。
- [ ] Session 只写 `session:<id>` 完整 snapshot且不传 Storage TTL。
- [ ] Memory 使用专用 ContentStore，不生成 root KV key。
- [ ] Restore 遇坏 snapshot 不发布部分状态。

## 门禁

- [ ] Storage 文档不存在未定义配置、后端、Session 子 key 或 Memory KV key。
- [ ] fenced SQL/YAML/Go 结构完整，相对链接存在。
- [ ] `git diff --check` 通过。

---

*最后更新: 2026-07-22*
