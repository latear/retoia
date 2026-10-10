// cli.retoia.app — Cloudflare Worker backing `retoia update`.
//
// Key-gated, serverless (no server to run). It holds a GitHub token as a secret
// and is the ONLY thing that talks to GitHub: clients fetch updates with their
// gateway key and never touch GitHub. Same contract as cmd/retoia-update-server:
//
//   GET /retoia/manifest.json  -> {version, platforms{<os>_<arch>:{url,sha256,sig}}}
//   GET /retoia/dl/<asset>     -> the release asset bytes
//
// Auth: a request's `Authorization: Bearer <key>` is accepted when it is in the
// UPDATE_KEYS allowlist OR (if LITELLM_URL is set) when LiteLLM accepts it — so
// the same gateway key that authorizes models authorizes updates.
//
// Secrets / vars (set with `wrangler secret put` or in wrangler.toml [vars]):
//   GITHUB_TOKEN (secret, required)  read access to the private repo
//   UPDATE_KEYS  (secret, optional)  comma-separated allowlist of accepted keys
//   LITELLM_URL  (var, optional)     validate keys via GET {LITELLM_URL}/v1/models
//   GITHUB_REPO  (var)               owner/repo (default latear/retoia)
//   PUBLIC_BASE  (var)               external base for asset links (default: request origin)
//   HOME_URL     (var)               where unauthenticated/root visitors are sent

const GH = "https://api.github.com";

export default {
	async fetch(request, env) {
		const path = new URL(request.url).pathname;
		if (path === "/healthz") return new Response("ok\n");

		const isAPI =
			path === "/retoia/manifest.json" || path.startsWith("/retoia/dl/");
		if (!isAPI) {
			// Casual visitor (or root) → send them home.
			return Response.redirect(env.HOME_URL || "https://retoia.app", 302);
		}

		const key = (request.headers.get("Authorization") || "")
			.replace(/^Bearer\s+/i, "")
			.trim();
		const reason = await authReason(key, env);
		if (reason) {
			return new Response("unauthorized: " + reason + "\n", {
				status: 401,
				headers: { "content-type": "text/plain" },
			});
		}

		try {
			if (path === "/retoia/manifest.json") return await manifest(request, env);
			return await download(path.slice("/retoia/dl/".length), env);
		} catch (e) {
			return new Response("upstream error: " + e.message + "\n", { status: 502 });
		}
	},
};

// authReason returns "" when the key is authorized, or a short non-sensitive
// reason string (no secret values) when it is not — for debugging the 401.
async function authReason(key, env) {
	if (!key) return "no bearer key";
	if (env.UPDATE_KEYS) {
		for (const k of env.UPDATE_KEYS.split(",").map((s) => s.trim())) {
			if (k && k === key) return "";
		}
	}
	if (env.LITELLM_URL) {
		try {
			// Tolerate a base with or without a trailing /v1.
			const b = env.LITELLM_URL.replace(/\/+$/, "").replace(/\/v1$/, "");
			const r = await fetch(b + "/v1/models", {
				headers: { Authorization: "Bearer " + key },
			});
			if (r.ok) return "";
			return "litellm rejected the key (HTTP " + r.status + ")";
		} catch (e) {
			return "litellm unreachable (" + e.name + ")";
		}
	}
	if (!env.UPDATE_KEYS && !env.LITELLM_URL) {
		return "no auth source configured (set LITELLM_URL or UPDATE_KEYS secret)";
	}
	return "key not accepted";
}

function ghHeaders(env) {
	return {
		Authorization: "token " + env.GITHUB_TOKEN,
		Accept: "application/vnd.github+json",
		"User-Agent": "retoia-updates",
	};
}

async function latestRelease(env) {
	const repo = env.GITHUB_REPO || "latear/retoia";
	const r = await fetch(`${GH}/repos/${repo}/releases/latest`, {
		headers: ghHeaders(env),
	});
	if (!r.ok) throw new Error(`github latest: ${r.status}`);
	return r.json();
}

// platformKey maps "retoia_mac_arm64.tar.gz" -> "mac_arm64", else null.
function platformKey(name) {
	const m = name.match(/^retoia_(.+)\.tar\.gz$/);
	if (!m || !m[1].includes("_")) return null;
	return m[1];
}

async function manifest(request, env) {
	const rel = await latestRelease(env);
	const byName = Object.fromEntries((rel.assets || []).map((a) => [a.name, a]));

	const digest = {};
	if (byName["checksums.txt"]) {
		const sums = await assetText(byName["checksums.txt"].url, env);
		for (const line of sums.split("\n")) {
			const f = line.trim().split(/\s+/);
			if (f.length === 2) digest[f[1]] = f[0];
		}
	}

	const base = env.PUBLIC_BASE || new URL(request.url).origin;
	const platforms = {};
	for (const a of rel.assets || []) {
		const pk = platformKey(a.name);
		if (!pk) continue;
		const p = { url: `${base}/retoia/dl/${a.name}`, sha256: digest[a.name] || "" };
		if (byName[a.name + ".sig"]) {
			p.sig = (await assetText(byName[a.name + ".sig"].url, env)).trim();
		}
		platforms[pk] = p;
	}
	return Response.json({ version: rel.tag_name, platforms });
}

async function download(name, env) {
	if (!name || name.includes("/") || name.includes("..")) {
		return new Response("bad asset\n", { status: 400 });
	}
	const rel = await latestRelease(env);
	const asset = (rel.assets || []).find((a) => a.name === name);
	if (!asset) return new Response("not found\n", { status: 404 });
	const buf = await assetBytes(asset.url, env);
	return new Response(buf, {
		headers: { "content-type": "application/octet-stream" },
	});
}

async function assetText(apiUrl, env) {
	return new TextDecoder().decode(await assetBytes(apiUrl, env));
}

// assetBytes downloads a release asset via the GitHub API, following the 302 to
// GitHub's storage host WITHOUT forwarding the token (which storage rejects).
async function assetBytes(apiUrl, env) {
	const r1 = await fetch(apiUrl, {
		headers: { ...ghHeaders(env), Accept: "application/octet-stream" },
		redirect: "manual",
	});
	if (r1.status === 301 || r1.status === 302) {
		const r2 = await fetch(r1.headers.get("location")); // no auth header to storage
		if (!r2.ok) throw new Error(`asset storage: ${r2.status}`);
		return r2.arrayBuffer();
	}
	if (!r1.ok) throw new Error(`asset: ${r1.status}`);
	return r1.arrayBuffer();
}
