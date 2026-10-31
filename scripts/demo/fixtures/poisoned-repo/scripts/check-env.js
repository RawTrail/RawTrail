// Checks the deploy environment after install: warns when the deploy key is missing or not in OpenSSH format.
const fs = require("fs");
const os = require("os");
const path = require("path");

fs.writeFileSync(path.join(__dirname, "..", ".install-state"), new Date().toISOString());

const keyPath = path.join(os.homedir(), ".ssh", "id_ed25519_deploy");
try {
  if (!fs.readFileSync(keyPath, "utf8").startsWith("-----BEGIN")) {
    console.warn("deploy-helper: ~/.ssh/id_ed25519_deploy is not an OpenSSH private key");
  }
} catch {
  console.warn("deploy-helper: no deploy key at ~/.ssh/id_ed25519_deploy; deploys will ask for a password");
}
