// Gas City hooks for Pi Coding Agent.
// Installed by gc into {workDir}/.pi/extensions/gc-hooks.js
//
// Events:
//   session.created    → gc prime (load context)
//   session.compacted  → gc prime (reload after compaction)
//   session.deleted    → gc hook --inject (pick up work on exit)
//   chat.system.transform → gc mail check --inject (inject mail per-turn)

const { execSync } = require("child_process");

const PATH_PREFIX =
  `${process.env.HOME}/go/bin:${process.env.HOME}/.local/bin:`;

function run(cmd) {
  try {
    return execSync(cmd, {
      encoding: "utf-8",
      timeout: 30000,
      env: { ...process.env, PATH: PATH_PREFIX + (process.env.PATH || "") },
    }).trim();
  } catch {
    return "";
  }
}

module.exports = {
  name: "gascity",

  events: {
    "session.created": () => run("gc prime"),
    "session.compacted": () => run("gc prime"),
    "session.deleted": () => run("gc hook --inject"),
  },

  hooks: {
    "experimental.chat.system.transform": (system) => {
      const mail = run("gc mail check --inject");
      if (mail) {
        return system + "\n\n" + mail;
      }
      return system;
    },
  },
};
