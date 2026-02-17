#!/usr/bin/env node

const { execSync } = require('node:child_process');
const { chromium } = require('@playwright/test');

const BASE_URL = process.env.APP_BASE_URL || 'http://localhost:3000';
const ROOT_DIR = process.cwd();

function randomEmail(prefix) {
  return `${prefix}-${Date.now()}-${Math.floor(Math.random() * 10000)}@example.com`;
}

async function waitForFeedback(page, timeoutMs = 6000) {
  const toast = page.locator('.toast').first();
  const statusError = page.locator('.status-bar .error').first();
  const statusMessage = page.locator('.status-bar .message').first();

  const checks = [
    toast.waitFor({ state: 'visible', timeout: timeoutMs }).then(() => 'toast').catch(() => ''),
    statusError.waitFor({ state: 'visible', timeout: timeoutMs }).then(() => 'status_error').catch(() => ''),
    statusMessage.waitFor({ state: 'visible', timeout: timeoutMs }).then(() => 'status_message').catch(() => '')
  ];

  const found = (await Promise.all(checks)).filter(Boolean);
  return found.length > 0 ? found[0] : '';
}

async function run() {
  const browser = await chromium.launch({
    headless: true,
    executablePath: '/usr/bin/chromium',
    args: ['--no-sandbox']
  });

  const issues = [];
  const working = [];

  const context = await browser.newContext();
  const page = await context.newPage();

  try {
    const email = randomEmail('ui-offline');
    const password = 'password123';

    await page.goto(`${BASE_URL}/`, { waitUntil: 'domcontentloaded' });
    await page.locator('input[type="email"]').fill(email);
    await page.locator('input[type="password"]').fill(password);
    await Promise.all([
      page.getByRole('button', { name: /^Notifications$/i }).waitFor({ timeout: 20000 }),
      page.getByRole('button', { name: /^Sign up$/i }).click()
    ]);

    await page.getByRole('button', { name: /^Create Persona$/ }).click();
    await page.waitForTimeout(1000);

    execSync('docker compose stop backend', { cwd: ROOT_DIR, stdio: 'ignore' });

    await page.getByRole('button', { name: /^Refresh Feed$/ }).click();
    const feedFeedback = await waitForFeedback(page, 7000);
    if (!feedFeedback) {
      issues.push({
        page: 'Home / Feed',
        button: 'Refresh Feed (backend offline)',
        expected: 'When backend is down, user should see explicit error feedback.',
        actual: 'No visible error feedback after refresh with backend offline.'
      });
    } else {
      working.push('Offline handling: Refresh Feed shows feedback when backend is down.');
    }

    await page.getByRole('button', { name: /^Create AI Draft$/ }).click();
    const draftFeedback = await waitForFeedback(page, 7000);
    if (!draftFeedback) {
      issues.push({
        page: 'Home / Header',
        button: 'Create AI Draft (backend offline)',
        expected: 'When backend is down, create draft should fail with explicit feedback.',
        actual: 'No visible error feedback for create draft with backend offline.'
      });
    } else {
      working.push('Offline handling: Create AI Draft shows feedback when backend is down.');
    }

    await page.getByRole('button', { name: /^Notifications$/ }).click();
    await page.locator('.notifications-panel').getByRole('button', { name: /^Refresh$/ }).click();
    const notifFeedback = await waitForFeedback(page, 7000);
    if (!notifFeedback) {
      issues.push({
        page: 'Notifications',
        button: 'Refresh (backend offline)',
        expected: 'Notifications refresh should show explicit feedback when backend is unavailable.',
        actual: 'No visible feedback for notifications refresh during backend outage.'
      });
    } else {
      working.push('Offline handling: Notifications refresh shows feedback when backend is down.');
    }

    execSync('docker compose start backend', { cwd: ROOT_DIR, stdio: 'ignore' });

    console.log(
      JSON.stringify(
        {
          generatedAt: new Date().toISOString(),
          issues,
          working
        },
        null,
        2
      )
    );

    await context.close();
    await browser.close();
  } catch (error) {
    try {
      execSync('docker compose start backend', { cwd: ROOT_DIR, stdio: 'ignore' });
    } catch (_err) {
      // ignore
    }
    await context.close();
    await browser.close();
    throw error;
  }
}

run().catch((error) => {
  console.error(error);
  process.exit(1);
});
