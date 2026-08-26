#!/usr/bin/env python3
"""Compare long-context Luna calls through gorouter and a direct OpenAI API.

The script uses only Python's standard library. It builds a 40K-80K-token-ish
prompt from safe source files in this repository, never prints API keys or the
model response, and reports latency, first-byte time, response size, and usage.

Example:
  python3 scripts/long_context_compare.py --tokens 60000 --modes nonstream stream \
    --targets both --setup-gorouter

`--setup-gorouter` creates a temporary OpenAI-compatible credential and model
route that point at `--direct-base`, then removes both when the run finishes.
"""

from __future__ import annotations

import argparse
import concurrent.futures
import hashlib
import json
import os
import statistics
import sys
import time
import uuid
from dataclasses import asdict, dataclass
from pathlib import Path
from typing import Any
from urllib.error import HTTPError, URLError
from urllib.parse import quote
from urllib.request import Request, urlopen


SAFE_SUFFIXES = {
    ".go", ".ts", ".tsx", ".js", ".mjs", ".sql", ".md", ".json",
    ".yaml", ".yml", ".css", ".html", ".sh",
}
SKIP_PARTS = {".git", "node_modules", "dist", "coverage", "vendor"}
SKIP_NAMES = {".env", "swagger.json", "docs.go", "package-lock.json"}
CHARS_PER_TOKEN = 3.8  # Calibrated against Luna usage for this mixed codebase.


@dataclass
class Result:
    target: str
    mode: str
    requested_tokens: int
    estimated_input_tokens: int
    status: int
    ok: bool
    header_ms: int
    first_byte_ms: int
    duration_ms: int
    response_bytes: int
    prompt_tokens: int | None
    completion_tokens: int | None
    response_sha256: str
    error: str


def load_env_file(path: Path) -> None:
    if not path.is_file():
        return
    for raw_line in path.read_text(encoding="utf-8").splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        key = key.strip()
        value = value.strip()
        if value[:1] == value[-1:] and value[:1] in {"'", '"'}:
            value = value[1:-1]
        if key:
            os.environ.setdefault(key, value)


def secret_from_env(primary: str, fallback: str | None = None) -> str:
    value = os.environ.get(primary, "")
    if not value and fallback:
        value = os.environ.get(fallback, "")
    if not value:
        names = primary if not fallback else f"{primary} or {fallback}"
        raise RuntimeError(f"missing API key environment variable: {names}")
    return value


def source_files(root: Path) -> list[Path]:
    selected: list[Path] = []
    for path in root.rglob("*"):
        if not path.is_file() or path.suffix.lower() not in SAFE_SUFFIXES:
            continue
        relative = path.relative_to(root)
        if path.name in SKIP_NAMES or any(part in SKIP_PARTS for part in relative.parts):
            continue
        selected.append(path)
    return sorted(selected, key=lambda item: str(item.relative_to(root)))


def build_prompt(root: Path, requested_tokens: int) -> tuple[str, int, int]:
    target_chars = int(requested_tokens * CHARS_PER_TOKEN)
    header = (
        "Analyze the repository snapshot below. Identify its architecture, request flow, "
        "concurrency behavior, resource risks, and three concrete improvements. Return "
        "exactly 250 plain words. Do not use tools and do not reproduce large code blocks.\n\n"
        "<repository_snapshot>\n"
    )
    footer = "\n</repository_snapshot>"
    budget = max(1, target_chars - len(header) - len(footer))
    chunks: list[str] = []
    for path in source_files(root):
        try:
            text = path.read_text(encoding="utf-8")
        except (OSError, UnicodeError):
            continue
        if not text.strip():
            continue
        relative = path.relative_to(root)
        chunks.append(f"\n--- file: {relative} ---\n{text}\n")
    if not chunks:
        raise RuntimeError(f"no safe source files found under {root}")
    body_parts: list[str] = []
    body_length = 0
    pass_number = 1
    while body_length < budget:
        for chunk in chunks:
            prefix = "" if pass_number == 1 else f"\n--- repeated snapshot pass {pass_number} ---\n"
            candidate = prefix + chunk
            remaining = budget - body_length
            body_parts.append(candidate[:remaining])
            body_length += min(len(candidate), remaining)
            if body_length >= budget:
                break
        pass_number += 1
    prompt = header + "".join(body_parts) + footer
    return prompt, round(len(prompt) / CHARS_PER_TOKEN), len(chunks)


def json_request(method: str, url: str, key: str, payload: dict[str, Any] | None = None, timeout: int = 60) -> tuple[int, bytes]:
    data = None if payload is None else json.dumps(payload, separators=(",", ":")).encode()
    request = Request(url, data=data, method=method)
    request.add_header("Authorization", f"Bearer {key}")
    if data is not None:
        request.add_header("Content-Type", "application/json")
    try:
        with urlopen(request, timeout=timeout) as response:
            return response.status, response.read()
    except HTTPError as error:
        return error.code, error.read(4096)


class TemporaryRoute:
    def __init__(self, admin_base: str, admin_key: str, upstream_base: str, upstream_key: str, upstream_model: str):
        suffix = uuid.uuid4().hex[:10]
        self.admin_base = admin_base.rstrip("/")
        self.admin_key = admin_key
        self.credential_id = ""
        self.model = f"longctx-luna-{suffix}"
        credential = {
            "name": f"long-context-probe-{suffix}",
            "provider": "openai-compatible",
            "kind": "api_key",
            "base_url": upstream_base.rstrip("/"),
            "api_key": upstream_key,
        }
        status, body = json_request("POST", f"{self.admin_base}/admin/credentials", admin_key, credential)
        if status != 201:
            raise RuntimeError(f"create temporary credential returned HTTP {status}: {safe_error(body)}")
        self.credential_id = str(json.loads(body)["id"])
        definition = {
            "name": self.model,
            "strategy": "priority",
            "upstream_model": upstream_model,
            "enabled": True,
            "routes": [{"credential_id": self.credential_id, "priority": 0, "weight": 1, "enabled": True}],
        }
        status, body = json_request("PUT", f"{self.admin_base}/admin/models/{quote(self.model)}", admin_key, definition)
        if status != 200:
            self.cleanup()
            raise RuntimeError(f"create temporary model route returned HTTP {status}: {safe_error(body)}")

    def cleanup(self) -> None:
        if self.model:
            json_request("DELETE", f"{self.admin_base}/admin/models/{quote(self.model)}", self.admin_key)
        if self.credential_id:
            json_request("DELETE", f"{self.admin_base}/admin/credentials/{quote(self.credential_id)}", self.admin_key)
            self.credential_id = ""


def safe_error(body: bytes) -> str:
    text = body.decode("utf-8", "replace")[:500].replace("\n", " ")
    try:
        parsed = json.loads(text)
        error = parsed.get("error", parsed) if isinstance(parsed, dict) else parsed
        if isinstance(error, dict):
            return str(error.get("message", error.get("code", "request failed")))[:300]
    except json.JSONDecodeError:
        pass
    return text[:300]


def usage_from_body(body: bytes, stream: bool) -> tuple[int | None, int | None, bool]:
    objects: list[dict[str, Any]] = []
    if stream:
        for line in body.splitlines():
            if not line.startswith(b"data:"):
                continue
            data = line[5:].strip()
            if not data or data == b"[DONE]":
                continue
            try:
                parsed = json.loads(data)
                if isinstance(parsed, dict):
                    objects.append(parsed)
            except json.JSONDecodeError:
                continue
        valid = b"data: [DONE]" in body or b"data:[DONE]" in body
    else:
        try:
            parsed = json.loads(body)
            if isinstance(parsed, dict):
                objects.append(parsed)
        except json.JSONDecodeError:
            pass
        valid = bool(objects and objects[-1].get("choices"))
    usage: dict[str, Any] = {}
    for item in objects:
        if isinstance(item.get("usage"), dict):
            usage = item["usage"]
    prompt = usage.get("prompt_tokens", usage.get("input_tokens"))
    completion = usage.get("completion_tokens", usage.get("output_tokens"))
    return int(prompt) if prompt is not None else None, int(completion) if completion is not None else None, valid


def chat_call(target: str, base: str, key: str, model: str, prompt: str, requested_tokens: int, estimated_tokens: int, stream: bool, timeout: int, probe_identifier: str) -> Result:
    mode = "stream" if stream else "nonstream"
    unique_prompt = prompt + f"\n\nProbe identifier: {probe_identifier}"
    payload = {
        "model": model,
        "stream": stream,
        "temperature": 0.2,
        "reasoning": {"effort": "low"},
        "messages": [{"role": "user", "content": unique_prompt}],
    }
    request = Request(f"{base.rstrip('/')}/chat/completions", data=json.dumps(payload, separators=(",", ":")).encode(), method="POST")
    request.add_header("Authorization", f"Bearer {key}")
    request.add_header("Content-Type", "application/json")
    started = time.perf_counter()
    header_ms = first_byte_ms = 0
    status = 0
    body = bytearray()
    error_text = ""
    try:
        with urlopen(request, timeout=timeout) as response:
            status = response.status
            header_ms = round((time.perf_counter() - started) * 1000)
            while True:
                chunk = response.read(64 * 1024)
                if not chunk:
                    break
                if not first_byte_ms:
                    first_byte_ms = round((time.perf_counter() - started) * 1000)
                body.extend(chunk)
    except HTTPError as error:
        status = error.code
        body.extend(error.read(4096))
        error_text = safe_error(bytes(body))
    except (URLError, TimeoutError, OSError) as error:
        error_text = f"{type(error).__name__}: {error}"
    duration_ms = round((time.perf_counter() - started) * 1000)
    prompt_tokens, completion_tokens, valid = usage_from_body(bytes(body), stream)
    ok = 200 <= status < 300 and valid
    if not ok and not error_text:
        error_text = safe_error(bytes(body))
    return Result(
        target=target, mode=mode, requested_tokens=requested_tokens,
        estimated_input_tokens=estimated_tokens, status=status, ok=ok,
        header_ms=header_ms, first_byte_ms=first_byte_ms,
        duration_ms=duration_ms, response_bytes=len(body),
        prompt_tokens=prompt_tokens, completion_tokens=completion_tokens,
        response_sha256=hashlib.sha256(body).hexdigest()[:16] if body else "",
        error=error_text,
    )


def run_group(target: str, base: str, key: str, model: str, prompt: str, requested_tokens: int, estimated_tokens: int, mode: str, concurrency: int, timeout: int, probe_identifiers: list[str]) -> list[Result]:
    with concurrent.futures.ThreadPoolExecutor(max_workers=concurrency) as executor:
        futures = [executor.submit(chat_call, target, base, key, model, prompt, requested_tokens, estimated_tokens, mode == "stream", timeout, probe_identifiers[index]) for index in range(concurrency)]
        return [future.result() for future in futures]


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--targets", choices=["direct", "gorouter", "both"], default="both")
    parser.add_argument("--tokens", type=int, nargs="+", default=[60000], help="Approximate input token targets (recommended: 40000-80000)")
    parser.add_argument("--modes", choices=["nonstream", "stream"], nargs="+", default=["nonstream", "stream"])
    parser.add_argument("--concurrency", type=int, default=1)
    parser.add_argument("--timeout", type=int, default=600)
    parser.add_argument("--repo", type=Path, default=Path(__file__).resolve().parents[1])
    parser.add_argument("--env-file", type=Path, default=Path(".env"))
    parser.add_argument("--direct-base", default="http://100.93.207.70:2456/v1")
    parser.add_argument("--direct-model", default="gpt-5.6-luna")
    parser.add_argument("--direct-key-env", default="OMNI_API_KEY")
    parser.add_argument("--gorouter-base", default="http://127.0.0.1:8090/v1")
    parser.add_argument("--gorouter-admin-base", default="http://127.0.0.1:8090")
    parser.add_argument("--gorouter-model", default="")
    parser.add_argument("--gorouter-key-env", default="MASTER_KEY")
    parser.add_argument("--setup-gorouter", action="store_true", help="Create and clean up a temporary route through the direct endpoint")
    parser.add_argument("--keep-route", action="store_true", help="Keep the temporary credential and model route")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    load_env_file(args.env_file)
    if args.concurrency < 1 or args.concurrency > 64:
        raise RuntimeError("--concurrency must be between 1 and 64")
    if any(tokens < 1000 or tokens > 200000 for tokens in args.tokens):
        raise RuntimeError("each --tokens value must be between 1000 and 200000")
    direct_key = secret_from_env(args.direct_key_env, "MASTER_KEY")
    gorouter_key = secret_from_env(args.gorouter_key_env) if args.targets in {"gorouter", "both"} else ""
    temporary: TemporaryRoute | None = None
    gorouter_model = args.gorouter_model
    if args.targets in {"gorouter", "both"} and args.setup_gorouter:
        temporary = TemporaryRoute(args.gorouter_admin_base, gorouter_key, args.direct_base, direct_key, args.direct_model)
        gorouter_model = temporary.model
        print(json.dumps({"event": "temporary_route_created", "model": gorouter_model}))
    if args.targets in {"gorouter", "both"} and not gorouter_model:
        raise RuntimeError("set --gorouter-model or use --setup-gorouter")
    targets = []
    if args.targets in {"direct", "both"}:
        targets.append(("direct", args.direct_base, direct_key, args.direct_model))
    if args.targets in {"gorouter", "both"}:
        targets.append(("gorouter", args.gorouter_base, gorouter_key, gorouter_model))
    all_results: list[Result] = []
    try:
        for requested_tokens in args.tokens:
            prompt, estimated_tokens, file_count = build_prompt(args.repo.resolve(), requested_tokens)
            print(json.dumps({"event": "prompt_built", "requested_tokens": requested_tokens, "estimated_tokens": estimated_tokens, "characters": len(prompt), "source_files": file_count}))
            for mode in args.modes:
                probe_identifiers = [f"{requested_tokens}-{mode}-{index + 1}-{uuid.uuid4().hex}" for index in range(args.concurrency)]
                for target, base, key, model in targets:
                    results = run_group(target, base, key, model, prompt, requested_tokens, estimated_tokens, mode, args.concurrency, args.timeout, probe_identifiers)
                    all_results.extend(results)
                    for result in results:
                        print(json.dumps(asdict(result), separators=(",", ":")))
        summary: dict[str, Any] = {"event": "summary", "successful": sum(result.ok for result in all_results), "requests": len(all_results), "groups": []}
        group_keys = sorted({(result.requested_tokens, result.mode, result.target) for result in all_results})
        for tokens, mode, target in group_keys:
            group = [result for result in all_results if (result.requested_tokens, result.mode, result.target) == (tokens, mode, target)]
            summary["groups"].append({
                "target": target, "mode": mode, "requested_tokens": tokens,
                "successful": sum(result.ok for result in group), "requests": len(group),
                "duration_p50_ms": round(statistics.median(result.duration_ms for result in group)),
                "first_byte_p50_ms": round(statistics.median(result.first_byte_ms for result in group)),
                "provider_prompt_tokens": [result.prompt_tokens for result in group],
            })
        print(json.dumps(summary, separators=(",", ":")))
        return 0 if all(result.ok for result in all_results) else 1
    finally:
        if temporary and not args.keep_route:
            temporary.cleanup()
            print(json.dumps({"event": "temporary_route_removed", "model": temporary.model}))


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except KeyboardInterrupt:
        raise SystemExit(130)
    except Exception as error:
        print(json.dumps({"event": "fatal", "error": str(error)}), file=sys.stderr)
        raise SystemExit(2)
