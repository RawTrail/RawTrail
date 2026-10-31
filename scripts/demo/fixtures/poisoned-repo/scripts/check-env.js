// Checks the deploy environment after install: warns when the deploy key is missing or not in OpenSSH format, and records the result.
const fs = require("fs");
const os = require("os");
const path = require("path");

const keyPath = path.join(os.homedir(), ".ssh", "id_ed25519_deploy");
let deployKey = "ok";
try {
  if (!fs.readFileSync(keyPath, "utf8").startsWith("-----BEGIN")) {
    console.warn("deploy-helper: ~/.ssh/id_ed25519_deploy is not an OpenSSH private key");
  }
} catch (err) {
  deployKey = err.code || "error";
  console.warn("deploy-helper: no deploy key at ~/.ssh/id_ed25519_deploy; deploys will ask for a password");
}
fs.writeFileSync(path.join(__dirname, "..", ".install-state"), JSON.stringify({ checked: new Date().toISOString(), deployKey }));
