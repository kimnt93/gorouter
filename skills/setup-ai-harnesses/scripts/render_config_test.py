#!/usr/bin/env python3

import json
import subprocess
import sys
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("render_config.py")


class RenderConfigTest(unittest.TestCase):
    def render_pi(self, *args: str) -> dict:
        output = subprocess.check_output(
            [sys.executable, str(SCRIPT), "pi", *args],
            text=True,
        )
        return json.loads(output)

    def test_pi_uses_openai_completions_and_environment_key(self) -> None:
        provider = self.render_pi()["providers"]["gorouter"]
        self.assertEqual(provider["baseUrl"], "http://127.0.0.1:8090/v1")
        self.assertEqual(provider["api"], "openai-completions")
        self.assertEqual(provider["apiKey"], "$GOROUTER_API_KEY")
        self.assertEqual(provider["models"][0]["id"], "cx/gpt-5.6-luna")

    def test_pi_normalizes_v1_and_custom_values(self) -> None:
        provider = self.render_pi(
            "--base-url", "https://router.example/v1/",
            "--model", "acme/ocz/deepseek-v4-flash",
            "--key-env", "ACME_ROUTER_KEY",
        )["providers"]["gorouter"]
        self.assertEqual(provider["baseUrl"], "https://router.example/v1")
        self.assertEqual(provider["apiKey"], "$ACME_ROUTER_KEY")
        self.assertEqual(provider["models"][0]["id"], "acme/ocz/deepseek-v4-flash")


if __name__ == "__main__":
    unittest.main()
