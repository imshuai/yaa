# Skill 部署边界

> 上级: [Skill 系统设计](README.md)

---

## 1. v1 没有运行时 Registry

Runtime 不下载、安装、更新、卸载或发布 Skill，也不持有 Registry URL、Token、缓存目录或安装记录。不存在 `Registry` interface、`InstallOptions`、后台更新任务、Skill 管理 Tool 或 mutation Remote API。

这样可以保持单一来源：Runtime 启动时实际读取的 `skills.dir`。网络 Registry、Git branch 和缓存状态不能在进程运行期间改变已验证的 Prompt/Tool 依赖。

## 2. 部署流程

Skill 包由部署系统在 Runtime 启动前放入目标目录：

1. 在 `skills.dir` 外下载或构建包。
2. 校验组织自己的来源、签名和许可证策略；Runtime v1 不定义通用信任库。
3. 解压到同文件系统的临时目录，拒绝 absolute path、`..`、symlink、device 和超限文件。
4. 确保目录中有且只有一个根 `SKILL.md`，再用原子 rename 发布为 `skills.dir/<name>`。
5. 启动或重启 Runtime；Skill Manager 重新执行完整解析和 binding。

覆盖已有目录也必须在 Runtime 停止时完成。运行中替换文件不会触发 reload，且正在运行的进程继续使用内存 snapshot。

## 3. 未来扩展条件

只有同时定义以下契约后，后续版本才能增加 Registry：

- 包格式、hash/signature 和 trust root；
- 防路径穿越/压缩炸弹的解包限制；
- 原子安装、回滚、并发锁和版本选择；
- Secret/token 存储与脱敏；
- Config、RBAC、Remote route/Tool schema 和审计事件；
- 在途 turn 与 Skill snapshot 的版本语义。

v1 不为这些未来能力保留空接口或配置字段。

---

*最后更新: 2026-07-22*
