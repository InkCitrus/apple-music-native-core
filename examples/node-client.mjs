import { spawn } from "node:child_process";
import { createInterface } from "node:readline";

// Usage: node examples/node-client.mjs /path/to/amcore /private/data-dir
const child = spawn(process.argv[2], ["--data-dir", process.argv[3], "serve"], { stdio: ["pipe", "pipe", "ignore"] });
const pending = new Map(); let id = 0;
createInterface({ input: child.stdout }).on("line", line => {
  let reply; try { reply = JSON.parse(line); } catch { return; }
  const item = pending.get(reply.id); if (!item) return;
  pending.delete(reply.id); clearTimeout(item.timer);
  if (reply.ok) item.resolve(reply.result); else item.reject(new Error(reply.error?.code || "unavailable"));
});
child.on("close", () => { for (const item of pending.values()) { clearTimeout(item.timer); item.reject(new Error("core exited")); } pending.clear(); });
function call(method, params = {}) {
  const request = ++id;
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => { pending.delete(request); reject(new Error("core timeout")); child.kill(); }, method === "auth" ? 15 * 60_000 : 65_000);
    pending.set(request, { resolve, reject, timer });
    child.stdin.write(JSON.stringify({ id: request, method, params }) + "\n");
  });
}
try {
  const hello = await call("hello");
  if (hello.protocol !== 1) throw new Error("incompatible protocol");
  console.log("Core protocol", hello.protocol);
  console.log(await call("doctor")); // contains presence flags, never tokens
  // await call("auth");
  // await call("play", { trackId: "YOUR_CATALOG_SONG_ID", quality: "hires" });
  // console.log(await call("state"));
  await call("close");
} finally { child.stdin.end(); }
