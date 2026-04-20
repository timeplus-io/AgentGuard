import argparse
import importlib.resources
import os
import shutil
import sys


def main() -> None:
    parser = argparse.ArgumentParser(
        prog="agentguard-hermes",
        description="AgentGuard security monitoring plugin for Hermes Agent",
    )
    sub = parser.add_subparsers(dest="command", required=True)

    install_cmd = sub.add_parser("install", help="Install the plugin into Hermes data directory")
    install_cmd.add_argument(
        "--hermes-dir",
        default=os.path.expanduser("~/.hermes"),
        metavar="DIR",
        help="Hermes data directory (default: ~/.hermes)",
    )

    args = parser.parse_args()

    if args.command == "install":
        _install(args.hermes_dir)


def _install(hermes_dir: str) -> None:
    plugin_dir = os.path.join(hermes_dir, "plugins", "agentguard")
    os.makedirs(plugin_dir, exist_ok=True)

    pkg = importlib.resources.files("agentguard_hermes")

    with importlib.resources.as_file(pkg / "plugin.yaml") as src:
        shutil.copy2(src, os.path.join(plugin_dir, "plugin.yaml"))

    with importlib.resources.as_file(pkg / "plugin.py") as src:
        shutil.copy2(src, os.path.join(plugin_dir, "__init__.py"))

    print(f"AgentGuard plugin installed → {plugin_dir}")
    print()
    print("Configure via environment variables before starting Hermes:")
    print()
    print("  AGENTGUARD_TIMEPLUS_URL     (default: http://localhost:3218)")
    print("  AGENTGUARD_USERNAME         (default: proton)")
    print("  AGENTGUARD_PASSWORD")
    print("  AGENTGUARD_AGENT_ID         (default: hostname)")
    print("  AGENTGUARD_DEPLOYMENT_ID    (default: local)")
    print("  AGENTGUARD_DEPLOYMENT_NAME  (default: Local Dev)")
    print("  AGENTGUARD_STREAM           (default: agentguard_hook_events)")


if __name__ == "__main__":
    main()
