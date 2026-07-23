# 前端功能测试：一键启动

1. 前置要求：Docker Desktop 已启动；已安装 Docker Compose v2；已准备智谱 API Key。

2. 进入项目目录：

   ```bash
   cd "/Users/Thomas/Desktop/AI Cloud Runtime Project/AI-Agent-Cloud-Runtime"
   ```

3. 首次创建环境文件：

   ```bash
   test -f .env || cp .env.example .env
   ```

4. 编辑 `.env`，仅确认或填写以下配置：

   ```dotenv
   LLM_PROVIDER=openai
   OPENAI_BASE_URL=https://open.bigmodel.cn/api/paas/v4
   OPENAI_API_KEY=你的智谱_API_KEY
   OPENAI_MODEL=glm-4.7-flash
   OPENAI_MAX_TOKENS=65536
   LLM_THINKING_ENABLED=true
   ```

5. 一键构建并启动所有服务：

   ```bash
   docker compose --env-file .env -f deploy/docker-compose.yml up -d --build
   ```

6. 等待并检查服务状态：

   ```bash
   docker compose --env-file .env -f deploy/docker-compose.yml ps
   ```

   ```text
   需要 running：agentforge-web、agentforge-controlplane、agentforge-gateway、agentforge-worker
   需要 healthy：agentforge-redis、agentforge-postgres
   ```

7. 打开前端：

   ```text
   http://localhost:5173
   ```

8. 前端测试顺序：

   ```text
   Agents → Create Agent → 填写名称/角色/系统提示词 → Create
   Agent Detail → Run task → 输入任务 → Run
   Agent Detail → Stop → Start → Delete
   Runs → 查看运行历史
   Workspace → 查看只读文件目录
   ```

9. 故障日志：

   ```bash
   docker compose --env-file .env -f deploy/docker-compose.yml logs -f web controlplane worker
   ```

10. 停止服务：

    ```bash
    docker compose --env-file .env -f deploy/docker-compose.yml down
    ```
