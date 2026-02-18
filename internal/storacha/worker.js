#!/usr/bin/env node
// Storacha worker process for handling uploads via IPC using CLI

const { execFile } = require('child_process');
const { promisify } = require('util');
const readline = require('readline');

const execFileAsync = promisify(execFile);

let currentSpace = null;
let initialized = false;

async function initialize(spaceDID) {
  try {
    // Set the space using storacha CLI
    await execFileAsync('storacha', ['space', 'use', spaceDID]);
    currentSpace = spaceDID;
    initialized = true;
    return { success: true };
  } catch (error) {
    return { success: false, error: error.message };
  }
}

async function uploadFile(filePath) {
  try {
    if (!initialized) {
      throw new Error('Worker not initialized');
    }
    
    // Upload using storacha CLI
    const { stdout, stderr } = await execFileAsync('storacha', ['up', filePath]);
    
    // Extract CID from output
    const output = stdout + stderr;
    const cid = extractCID(output);
    
    if (!cid) {
      throw new Error(`Could not extract CID from output: ${output}`);
    }
    
    return { success: true, cid: cid };
  } catch (error) {
    return { success: false, error: error.message };
  }
}

function extractCID(output) {
  // Try different CID patterns
  const patterns = [
    /bafy[a-zA-Z0-9]{50,}/,
    /bafk[a-zA-Z0-9]{50,}/,
    /ipfs\/(bafy[a-zA-Z0-9]+|bafk[a-zA-Z0-9]+)/
  ];
  
  for (const pattern of patterns) {
    const match = output.match(pattern);
    if (match) {
      return match[1] || match[0];
    }
  }
  
  // Try line by line
  const lines = output.split('\n');
  for (const line of lines) {
    const trimmed = line.trim();
    if (trimmed.startsWith('bafy') || trimmed.startsWith('bafk')) {
      return trimmed;
    }
  }
  
  return null;
}

async function handleRequest(request) {
  try {
    const req = JSON.parse(request);
    
    switch (req.action) {
      case 'init':
        return await initialize(req.spaceDID);
      
      case 'upload':
        return await uploadFile(req.path);
      
      case 'ping':
        return { success: true, message: 'pong' };
      
      case 'shutdown':
        process.exit(0);
      
      default:
        return { success: false, error: `Unknown action: ${req.action}` };
    }
  } catch (error) {
    return { success: false, error: error.message };
  }
}

// Set up readline interface for JSON-based IPC
const rl = readline.createInterface({
  input: process.stdin,
  output: process.stdout,
  terminal: false
});

rl.on('line', async (line) => {
  const response = await handleRequest(line.trim());
  console.log(JSON.stringify(response));
});

process.on('uncaughtException', (error) => {
  console.log(JSON.stringify({ success: false, error: error.message }));
});

process.on('unhandledRejection', (error) => {
  console.log(JSON.stringify({ success: false, error: error.message }));
});

console.log(JSON.stringify({ success: true, message: 'worker ready' }));
