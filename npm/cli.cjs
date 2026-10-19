#!/usr/bin/env node
'use strict';

const { spawn } = require('node:child_process');
const path = require('node:path');

const binary = process.platform === 'win32' ? 'obramat.exe' : 'obramat';
const child = spawn(path.join(__dirname, binary), process.argv.slice(2), { stdio: 'inherit' });
const handlers = new Map();
for (const signal of ['SIGINT', 'SIGTERM', 'SIGHUP']) {
  const handler = () => child.kill(signal);
  handlers.set(signal, handler);
  process.on(signal, handler);
}

child.once('error', error => {
  if (error.code === 'ENOENT') {
    console.error('obramat-cli: the release binary is missing. Enable install scripts and run npm rebuild obramat-cli.');
  } else {
    console.error(`obramat-cli: could not start the release binary: ${error.message}`);
  }
  process.exitCode = 1;
});
child.once('close', (code, signal) => {
  for (const [name, handler] of handlers) process.removeListener(name, handler);
  if (signal) {
    process.kill(process.pid, signal);
  } else {
    process.exitCode = process.exitCode || code || 0;
  }
});
