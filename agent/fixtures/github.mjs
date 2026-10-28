export const HEAD = "a".repeat(40), BASE = "b".repeat(40);
export const SOURCE = "export const answer = 42;\n";
export function githubFixture(options = {}) {
  const state = { permission: "write", head: HEAD, base: BASE, repository: "owner/repo", prState: "open", failWrite: false, ...options };
  const requests = [];
  const files = options.files || [{ filename: "src/a.ts", status: "modified", patch: "@@ -1 +1 @@\n-old\n+new" }];
  const transport = async (url, request) => {
    const route = url.pathname, method = request.method;
    requests.push({ route, method, body: request.body ? JSON.parse(request.body) : undefined, url: String(url) });
    if (request.signal.aborted) throw request.signal.reason;
    if (request.headers.Authorization !== "Bearer fake-github-key") throw new Error("Unexpected fixture credential");
    const response = (value) => new Response(JSON.stringify(value), { headers: { "Content-Type": "application/json" } });
    if (route.endsWith("/permission")) return response({ permission: state.permission });
    if (route === "/repos/owner/repo/pulls/7" && method === "GET") return response({
      number: 7, state: state.prState, title: "Fixture PR", body: "Untrusted PR body",
      head: { sha: state.head, repo: { full_name: state.repository } }, base: { sha: state.base }, changed_files: files.length,
    });
    if (route.endsWith("/pulls/7/files")) {
      const page = Number(url.searchParams.get("page"));
      return response(files.slice((page-1)*100, page*100));
    }
    if (route.includes("/contents/")) return response({ type: "file", encoding: "base64", size: Buffer.byteLength(SOURCE), content: Buffer.from(SOURCE).toString("base64") });
    if (route === "/repos/owner/repo/issues/7" && method === "GET") return response({ number: 7, title: "Fixture issue", body: "Implement a focused improvement." });
    if (route === "/repos/owner/repo") return response({ default_branch: "main" });
    if (route === "/repos/owner/repo/commits/main") return response({ sha: HEAD });
    if (route.endsWith("/comments") && method === "GET") return response([{ id: 1, user: { login: "maintainer" }, body: "Latest requirement." }]);
    if ((route.endsWith("/reviews") || route.endsWith("/comments")) && method === "POST") {
      if (state.failWrite) throw new Error("Ambiguous fixture write failure");
      return response({ id: 123, html_url: "https://github.invalid/owner/repo/pull/7#receipt" });
    }
    throw new Error("Unexpected fixture route: " + method + " " + route);
  };
  return { state, requests, fetch: transport, options: {
    repository: "owner/repo", actor: "maintainer", eventName: "workflow_dispatch", number: 7,
    token: "fake-github-key", capabilities: "read,review-propose", fetch: transport,
  } };
}
