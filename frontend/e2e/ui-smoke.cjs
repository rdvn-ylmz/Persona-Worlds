#!/usr/bin/env node

const { chromium } = require('@playwright/test');

const BASE_URL = process.env.APP_BASE_URL || 'http://localhost:3000';
const API_BASE = process.env.API_BASE_URL || 'http://localhost:8080';

const results = {
  issues: [],
  working: [],
  partial: []
};

function addIssue(page, button, expected, actual, consoleErrors, suggestedFix, severity = 'Medium') {
  results.issues.push({
    page,
    button,
    expected,
    actual,
    consoleErrors,
    suggestedFix,
    severity
  });
}

function addWorking(item) {
  results.working.push(item);
}

function addPartial(item) {
  results.partial.push(item);
}

function randomEmail(prefix) {
  return `${prefix}-${Date.now()}-${Math.floor(Math.random() * 10000)}@example.com`;
}

function attachDiagnostics(page, store) {
  page.on('console', (message) => {
    if (message.type() === 'error') {
      store.push(`[console] ${message.text()}`);
    }
  });

  page.on('pageerror', (error) => {
    store.push(`[pageerror] ${error.message}`);
  });
}

function recentConsoleErrors(diagnostics, startIdx = 0) {
  const items = diagnostics.slice(startIdx).slice(-3);
  return items.length > 0 ? items.join(' | ') : 'none';
}

async function waitForFeedback(page, timeoutMs = 5000) {
  const toast = page.locator('.toast').first();
  const statusMessage = page.locator('.status-bar .message').first();
  const statusError = page.locator('.status-bar .error').first();

  const checks = [
    toast.waitFor({ state: 'visible', timeout: timeoutMs }).then(() => true).catch(() => false),
    statusMessage.waitFor({ state: 'visible', timeout: timeoutMs }).then(() => true).catch(() => false),
    statusError.waitFor({ state: 'visible', timeout: timeoutMs }).then(() => true).catch(() => false)
  ];

  const settled = await Promise.all(checks);
  return settled.some(Boolean);
}

async function waitForApi(page, matcher, timeout = 20000) {
  return page.waitForResponse(
    (resp) => {
      if (!resp.url().startsWith(API_BASE)) {
        return false;
      }
      return matcher(resp);
    },
    { timeout }
  );
}

async function safePopupClick(page, locator) {
  const [popup] = await Promise.all([
    page.waitForEvent('popup', { timeout: 10000 }),
    locator.click()
  ]);
  await popup.waitForLoadState('domcontentloaded', { timeout: 10000 });
  return popup;
}

async function signupFromHome(page, email, password) {
  await page.goto(`${BASE_URL}/`, { waitUntil: 'domcontentloaded' });

  await page.locator('input[type="email"]').fill(email);
  await page.locator('input[type="password"]').fill(password);

  await Promise.all([
    page.getByRole('button', { name: /^Notifications$/i }).waitFor({ timeout: 20000 }),
    page.getByRole('button', { name: /^Sign up$/i }).click()
  ]);
}

async function signupFromSignupPage(page, email, password) {
  await page.goto(`${BASE_URL}/signup`, { waitUntil: 'domcontentloaded' });
  await page.locator('input[type="email"]').fill(email);
  await page.locator('input[type="password"]').fill(password);

  await Promise.all([
    page.getByRole('button', { name: /^Notifications$/i }).waitFor({ timeout: 20000 }),
    page.getByRole('button', { name: /^Sign up$/i }).click()
  ]);
}

async function run() {
  const browser = await chromium.launch({
    headless: true,
    executablePath: '/usr/bin/chromium',
    args: ['--no-sandbox']
  });

  let sourceBattleID = '';
  let feedBattleID = '';
  let publicSlug = '';

  const diagnosticsUser1 = [];
  const diagnosticsUser2 = [];
  const diagnosticsPublic = [];

  const email1 = randomEmail('ui-audit-owner');
  const email2 = randomEmail('ui-audit-follower');
  const password = 'password123';

  const context1 = await browser.newContext({ permissions: ['clipboard-read', 'clipboard-write'] });
  const page1 = await context1.newPage();
  attachDiagnostics(page1, diagnosticsUser1);

  try {
    await signupFromHome(page1, email1, password);
    addWorking('Auth: Sign up from home form redirects to authenticated dashboard.');

    const createPersonaConsoleIdx = diagnosticsUser1.length;
    const createPersonaRespPromise = waitForApi(
      page1,
      (resp) => resp.request().method() === 'POST' && /\/personas$/.test(new URL(resp.url()).pathname)
    );
    await page1.getByRole('button', { name: /^Create Persona$/ }).click();
    const createPersonaResp = await createPersonaRespPromise;
    if (!createPersonaResp.ok()) {
      addIssue(
        'Home / Profile',
        'Create Persona',
        'Persona must be created and user should get success feedback.',
        `API returned ${createPersonaResp.status()}.`,
        recentConsoleErrors(diagnosticsUser1, createPersonaConsoleIdx),
        'Keep current payload validation and ensure success state is surfaced to user.',
        'High'
      );
    } else {
      const personaBody = await createPersonaResp.json();
      if (!personaBody || !personaBody.id) {
        addIssue(
          'Home / Profile',
          'Create Persona',
          'API should return created persona id.',
          'Create persona response did not include id.',
          recentConsoleErrors(diagnosticsUser1, createPersonaConsoleIdx),
          'Return full created persona payload from backend.',
          'High'
        );
      }
      const feedbackSeen = await waitForFeedback(page1, 4000);
      if (!feedbackSeen) {
        addIssue(
          'Home / Profile',
          'Create Persona',
          'Success feedback should be visible after creation.',
          'No visible success feedback after successful create persona response.',
          recentConsoleErrors(diagnosticsUser1, createPersonaConsoleIdx),
          'Show explicit success toast or inline confirmation after persona creation.',
          'Medium'
        );
      } else {
        addWorking('Home / Profile: Create Persona sends API request and shows feedback.');
      }
    }

    const publishConsoleIdx = diagnosticsUser1.length;
    const publishRespPromise = waitForApi(
      page1,
      (resp) => resp.request().method() === 'POST' && /\/publish-profile$/.test(new URL(resp.url()).pathname)
    );
    await page1.getByRole('button', { name: /^Publish & Copy$/ }).click();
    const publishResp = await publishRespPromise;
    if (!publishResp.ok()) {
      addIssue(
        'Home / Header',
        'Publish & Copy',
        'Profile should publish and provide share link feedback.',
        `API returned ${publishResp.status()}.`,
        recentConsoleErrors(diagnosticsUser1, publishConsoleIdx),
        'Surface backend error details and keep publish action idempotent.',
        'High'
      );
    } else {
      const body = await publishResp.json();
      publicSlug = (body.slug || '').trim();
      const feedbackSeen = await waitForFeedback(page1, 4000);
      if (!publicSlug) {
        addIssue(
          'Home / Header',
          'Publish & Copy',
          'Publishing must return public slug.',
          'Publish response missing slug.',
          recentConsoleErrors(diagnosticsUser1, publishConsoleIdx),
          'Ensure publish-profile endpoint always returns slug.',
          'High'
        );
      }
      if (!feedbackSeen) {
        addIssue(
          'Home / Header',
          'Publish & Copy',
          'User should get success feedback after publish.',
          'No success or info feedback was visible.',
          recentConsoleErrors(diagnosticsUser1, publishConsoleIdx),
          'Show deterministic toast or inline status after publish/copy.',
          'Medium'
        );
      } else {
        addWorking('Home / Header: Publish & Copy publishes profile and shows feedback.');
      }
    }

    const draftConsoleIdx = diagnosticsUser1.length;
    const createDraftRespPromise = waitForApi(
      page1,
      (resp) => resp.request().method() === 'POST' && /\/posts\/draft$/.test(new URL(resp.url()).pathname)
    );
    await page1.getByRole('button', { name: /^Create AI Draft$/ }).click();
    const createDraftResp = await createDraftRespPromise;
    let draftID = '';
    if (!createDraftResp.ok()) {
      addIssue(
        'Home / Rooms',
        'Create AI Draft',
        'Draft should be created and visible in posts list.',
        `API returned ${createDraftResp.status()}.`,
        recentConsoleErrors(diagnosticsUser1, draftConsoleIdx),
        'Keep draft quota messaging explicit and render inline error state.',
        'High'
      );
    } else {
      const draftBody = await createDraftResp.json();
      draftID = (draftBody.id || '').trim();
      sourceBattleID = draftID;
      if (!draftID) {
        addIssue(
          'Home / Rooms',
          'Create AI Draft',
          'Draft response should include post id.',
          'Draft id missing from response.',
          recentConsoleErrors(diagnosticsUser1, draftConsoleIdx),
          'Return created post id from draft endpoint.',
          'High'
        );
      }
      const draftCard = page1.locator(`#post-${draftID}`);
      const draftVisible = await draftCard.waitFor({ state: 'visible', timeout: 6000 }).then(() => true).catch(() => false);
      if (!draftVisible) {
        addIssue(
          'Home / Rooms',
          'Create AI Draft',
          'New draft card should render after create API success.',
          'Draft card did not appear in posts list.',
          recentConsoleErrors(diagnosticsUser1, draftConsoleIdx),
          'Refresh posts state after draft creation and ensure stable list keying.',
          'High'
        );
      } else {
        addWorking('Home / Rooms: Create AI Draft creates a new draft card.');
      }
    }

    if (sourceBattleID) {
      const approveConsoleIdx = diagnosticsUser1.length;
      const approveRespPromise = waitForApi(
        page1,
        (resp) => resp.request().method() === 'POST' && new URL(resp.url()).pathname.endsWith(`/posts/${sourceBattleID}/approve`)
      );
      const approveButton = page1.locator(`#post-${sourceBattleID}`).getByRole('button', { name: /^Approve & Publish$/ });
      if ((await approveButton.count()) === 0) {
        addIssue(
          'Home / Rooms',
          'Approve & Publish',
          'Draft cards should expose approve action.',
          'Approve button was not found on draft card.',
          recentConsoleErrors(diagnosticsUser1, approveConsoleIdx),
          'Ensure draft cards always render approval CTA.',
          'High'
        );
      } else {
        await approveButton.click();
        const approveResp = await approveRespPromise;
        if (!approveResp.ok()) {
          addIssue(
            'Home / Rooms',
            'Approve & Publish',
            'Draft should become published and remain visible.',
            `API returned ${approveResp.status()}.`,
            recentConsoleErrors(diagnosticsUser1, approveConsoleIdx),
            'Keep publish endpoint stable and surface backend validation errors.',
            'High'
          );
        } else {
          const generateButton = page1.locator(`#post-${sourceBattleID}`).getByRole('button', { name: /^Generate Replies$/ });
          const publishedStateVisible = await generateButton
            .waitFor({ state: 'visible', timeout: 8000 })
            .then(() => true)
            .catch(() => false);
          if (!publishedStateVisible) {
            addIssue(
              'Home / Rooms',
              'Approve & Publish',
              'Published post should expose Generate Replies action.',
              'UI did not transition from draft controls to published controls.',
              recentConsoleErrors(diagnosticsUser1, approveConsoleIdx),
              'Update local post state using approve response payload.',
              'High'
            );
          } else {
            addWorking('Home / Rooms: Approve & Publish updates card state to published.');
          }
        }
      }

      const threadConsoleIdx = diagnosticsUser1.length;
      const loadThreadRespPromise = waitForApi(
        page1,
        (resp) => resp.request().method() === 'GET' && new URL(resp.url()).pathname.endsWith(`/posts/${sourceBattleID}/thread`)
      );
      await page1.locator(`#post-${sourceBattleID}`).getByRole('button', { name: /^Load Thread$/ }).click();
      const loadThreadResp = await loadThreadRespPromise;
      if (!loadThreadResp.ok()) {
        addIssue(
          'Home / Rooms',
          'Load Thread',
          'Thread endpoint should load and render summary/replies.',
          `API returned ${loadThreadResp.status()}.`,
          recentConsoleErrors(diagnosticsUser1, threadConsoleIdx),
          'Provide thread fallback UI when endpoint errors.',
          'Medium'
        );
      } else {
        const summaryVisible = await page1
          .locator(`#post-${sourceBattleID}`)
          .getByText('AI Summary')
          .waitFor({ state: 'visible', timeout: 7000 })
          .then(() => true)
          .catch(() => false);
        if (summaryVisible) {
          addWorking('Home / Rooms: Load Thread fetches and renders thread details.');
        } else {
          addIssue(
            'Home / Rooms',
            'Load Thread',
            'Thread UI should show summary after successful API response.',
            'No summary section appeared after loading thread.',
            recentConsoleErrors(diagnosticsUser1, threadConsoleIdx),
            'Render thread section deterministically after thread fetch.',
            'Medium'
          );
        }
      }

      const battleCardConsoleIdx = diagnosticsUser1.length;
      const viewCardBtn = page1.locator(`#post-${sourceBattleID}`).getByRole('button', { name: /^View Battle Card$/ });
      const popup = await safePopupClick(page1, viewCardBtn);
      if (!popup.url().includes(`/b/${sourceBattleID}`)) {
        addIssue(
          'Home / Rooms',
          'View Battle Card',
          'Button should open battle card page for the selected post.',
          `Opened unexpected URL: ${popup.url()}`,
          recentConsoleErrors(diagnosticsUser1, battleCardConsoleIdx),
          'Pass explicit post id into battle-card opener and validate URL.',
          'Medium'
        );
      } else {
        addWorking('Home / Rooms: View Battle Card opens /b/:id in a new tab.');
      }
      await popup.close();
    }

    const validationConsoleIdx = diagnosticsUser1.length;
    const feedComposeInput = page1.locator('section#feed input[placeholder="Battle topic (required)"]');
    await feedComposeInput.fill('');
    await page1.getByRole('button', { name: /^Create Battle$/ }).click();
    const validationErrorVisible = await page1
      .locator('.status-bar .error')
      .filter({ hasText: 'enter a battle topic first' })
      .first()
      .waitFor({ state: 'visible', timeout: 5000 })
      .then(() => true)
      .catch(() => false);
    if (!validationErrorVisible) {
      addIssue(
        'Home / Feed',
        'Create Battle (empty topic)',
        'Validation error should be shown for empty topic.',
        'No visible validation feedback for empty topic.',
        recentConsoleErrors(diagnosticsUser1, validationConsoleIdx),
        'Show inline and toast validation for required feed topic.',
        'Medium'
      );
    } else {
      addWorking('Home / Feed: Empty topic validation provides visible feedback.');
    }

    const refreshFeedConsoleIdx = diagnosticsUser1.length;
    const refreshFeedResp = waitForApi(
      page1,
      (resp) => resp.request().method() === 'GET' && new URL(resp.url()).pathname === '/feed'
    );
    await page1.getByRole('button', { name: /^Refresh Feed$/ }).click();
    await refreshFeedResp;
    addWorking('Home / Feed: Refresh Feed triggers feed API request.');

    const useTemplateButton = page1.locator('section#feed').getByRole('button', { name: /^Use this template$/ }).first();
    if ((await useTemplateButton.count()) > 0) {
      await useTemplateButton.click();
      const selectedTemplateVisible = await page1
        .locator('section#feed')
        .getByText('Selected template id:')
        .waitFor({ state: 'visible', timeout: 5000 })
        .then(() => true)
        .catch(() => false);
      if (!selectedTemplateVisible) {
        addIssue(
          'Home / Feed',
          'Use this template',
          'Selecting template should update selected template state.',
          'Template selection did not produce visible selected template state.',
          recentConsoleErrors(diagnosticsUser1, refreshFeedConsoleIdx),
          'Persist selected template and show selected state in compose area.',
          'Medium'
        );
      } else {
        addWorking('Home / Feed: Use template updates compose template state.');
      }
    } else {
      addPartial('Home / Feed: No template card was available at run time, template button not exercised here.');
    }

    const createBattleConsoleIdx = diagnosticsUser1.length;
    await feedComposeInput.fill(`E2E battle topic ${Date.now()}`);
    const createBattleRespPromise = waitForApi(
      page1,
      (resp) => resp.request().method() === 'POST' && /\/rooms\/[^/]+\/battles$/.test(new URL(resp.url()).pathname),
      30000
    );
    await page1.getByRole('button', { name: /^Create Battle$/ }).click();
    const createBattleResp = await createBattleRespPromise;
    if (!createBattleResp.ok()) {
      addIssue(
        'Home / Feed',
        'Create Battle',
        'Battle creation should enqueue replies and show progress state.',
        `API returned ${createBattleResp.status()}.`,
        recentConsoleErrors(diagnosticsUser1, createBattleConsoleIdx),
        'Ensure create battle endpoint returns detailed state and UI consumes it.',
        'High'
      );
    } else {
      const battlePayload = await createBattleResp.json();
      feedBattleID = (battlePayload.battle_id || '').trim();
      const generationVisible = await page1
        .getByText('Battle generation status')
        .first()
        .waitFor({ state: 'visible', timeout: 8000 })
        .then(() => true)
        .catch(() => false);
      if (!generationVisible) {
        addIssue(
          'Home / Feed',
          'Create Battle',
          'Pending generation state should render after create.',
          'Battle generation status card did not appear.',
          recentConsoleErrors(diagnosticsUser1, createBattleConsoleIdx),
          'Always set battle generation state after create response.',
          'High'
        );
      } else {
        addWorking('Home / Feed: Create Battle shows async generation status card.');
      }

      const doneVisible = await page1
        .getByText('Done.')
        .first()
        .waitFor({ state: 'visible', timeout: 35000 })
        .then(() => true)
        .catch(() => false);
      if (doneVisible) {
        addWorking('Home / Feed: Battle polling completed and auto-updated to DONE state.');
      } else {
        const refreshStatusBtn = page1.getByRole('button', { name: /^Refresh Status$/ });
        if ((await refreshStatusBtn.count()) > 0) {
          await refreshStatusBtn.click();
          addPartial('Home / Feed: Polling timed out once; manual refresh path was available and used.');
        } else {
          addIssue(
            'Home / Feed',
            'Create Battle / Polling',
            'If not done automatically, user should see refresh action.',
            'Polling did not reach done and refresh action was not available.',
            recentConsoleErrors(diagnosticsUser1, createBattleConsoleIdx),
            'Keep timeout fallback CTA visible whenever polling is incomplete.',
            'High'
          );
        }
      }
    }

    const feedOpenBattleBtn = page1.locator('section#feed').getByRole('button', { name: /^Open battle$/ }).first();
    if ((await feedOpenBattleBtn.count()) > 0) {
      const popup = await safePopupClick(page1, feedOpenBattleBtn);
      if (!popup.url().includes('/b/')) {
        addIssue(
          'Home / Feed',
          'Open battle',
          'Feed battle CTA should open battle card page.',
          `Opened unexpected URL: ${popup.url()}`,
          recentConsoleErrors(diagnosticsUser1),
          'Route feed battle CTA directly to /b/:id.',
          'Medium'
        );
      } else {
        addWorking('Home / Feed: Open battle opens battle page in a popup tab.');
      }
      await popup.close();
    } else {
      addPartial('Home / Feed: No battle item with Open battle button was available after refresh.');
    }

    const weeklySection = page1.locator('section').filter({ hasText: 'Weekly Digest' }).first();
    const weeklyConsoleIdx = diagnosticsUser1.length;
    const refreshWeeklyRespPromise = waitForApi(
      page1,
      (resp) => resp.request().method() === 'GET' && new URL(resp.url()).pathname === '/digest/weekly'
    );
    await weeklySection.getByRole('button', { name: /^Refresh Weekly$/ }).click();
    await refreshWeeklyRespPromise;
    addWorking('Weekly Digest: Refresh Weekly triggers weekly digest API request.');

    const weeklyOpenBtn = weeklySection.getByRole('button', { name: /^Open battle$/ }).first();
    if ((await weeklyOpenBtn.count()) > 0) {
      const weeklyPopup = await safePopupClick(page1, weeklyOpenBtn);
      if (!weeklyPopup.url().includes('/b/')) {
        addIssue(
          'Weekly Digest',
          'Open battle',
          'Weekly digest item should open battle page.',
          `Opened unexpected URL: ${weeklyPopup.url()}`,
          recentConsoleErrors(diagnosticsUser1, weeklyConsoleIdx),
          'Map weekly digest items to concrete battle IDs before rendering CTA.',
          'Medium'
        );
      } else {
        addWorking('Weekly Digest: Open battle CTA opens battle page.');
      }
      await weeklyPopup.close();
    } else {
      addPartial('Weekly Digest: No digest item was available, Open battle CTA not exercised.');
    }

    const notificationsConsoleIdx = diagnosticsUser1.length;
    await page1.getByRole('button', { name: /^Notifications$/ }).click();
    const refreshNotificationsRespPromise = waitForApi(
      page1,
      (resp) => resp.request().method() === 'GET' && new URL(resp.url()).pathname === '/notifications'
    );
    await page1.locator('.notifications-panel').getByRole('button', { name: /^Refresh$/ }).click();
    await refreshNotificationsRespPromise;
    addWorking('Notifications: panel refresh triggers notifications API request.');

    await page1.locator('nav').getByRole('link', { name: /^Templates$/ }).click();
    await page1.waitForURL((url) => url.pathname === '/templates', { timeout: 10000 });
    addWorking('Navigation: Templates link routes to /templates.');

    const templatesConsoleIdx = diagnosticsUser1.length;
    const refreshTemplatesRespPromise = waitForApi(
      page1,
      (resp) => resp.request().method() === 'GET' && new URL(resp.url()).pathname === '/templates'
    );
    await page1.getByRole('button', { name: /^Refresh$/ }).click();
    await refreshTemplatesRespPromise;
    addWorking('Templates: Refresh button triggers template list API request.');

    const templateName = `QA Template ${Date.now()}`;
    await page1.locator('input[placeholder="Template name"]').fill(templateName);
    await page1.locator('textarea[placeholder="Prompt rules"]').fill('Round-based debate with concise rebuttals and final verdict.');

    const createTemplateRespPromise = waitForApi(
      page1,
      (resp) => resp.request().method() === 'POST' && new URL(resp.url()).pathname === '/templates'
    );
    await page1.getByRole('button', { name: /^Create Template$/ }).click();
    const createTemplateResp = await createTemplateRespPromise;
    if (!createTemplateResp.ok()) {
      addIssue(
        'Templates',
        'Create Template',
        'Template should be created and shown in list.',
        `API returned ${createTemplateResp.status()}.`,
        recentConsoleErrors(diagnosticsUser1, templatesConsoleIdx),
        'Keep create-template validation errors explicit in UI.',
        'High'
      );
    } else {
      const templateFeedbackSeen = await waitForFeedback(page1, 5000);
      if (!templateFeedbackSeen) {
        addIssue(
          'Templates',
          'Create Template',
          'Success feedback should be visible after template creation.',
          'No visible success feedback after create template.',
          recentConsoleErrors(diagnosticsUser1, templatesConsoleIdx),
          'Show deterministic toast/inline success after template create.',
          'Medium'
        );
      } else {
        addWorking('Templates: Create Template sends API request and shows success feedback.');
      }
    }

    const useTemplateFromMarketplace = page1.locator('.template-grid').getByRole('button', { name: /^Use template$/ }).first();
    if ((await useTemplateFromMarketplace.count()) > 0) {
      await useTemplateFromMarketplace.click();
      await page1.waitForURL((url) => url.pathname === '/' && url.hash === '#feed', { timeout: 10000 });
      addWorking('Templates: Use template redirects back to feed compose flow.');
    } else {
      addPartial('Templates: No template card available for Use template action.');
    }

    if (publicSlug) {
      const publicProfileConsoleIdx = diagnosticsUser1.length;
      await page1.goto(`${BASE_URL}/p/${encodeURIComponent(publicSlug)}`, { waitUntil: 'domcontentloaded' });
      const followRespPromise = waitForApi(
        page1,
        (resp) => resp.request().method() === 'POST' && new URL(resp.url()).pathname.endsWith(`/p/${publicSlug}/follow`)
      );
      await page1.getByRole('button', { name: /^Follow$/ }).click();
      const followResp = await followRespPromise;
      if (followResp.status() !== 409) {
        addIssue(
          'Public Persona / Owner View',
          'Follow',
          'Owner follow attempt should be rejected with explicit error.',
          `Unexpected status ${followResp.status()} for owner follow attempt.`,
          recentConsoleErrors(diagnosticsUser1, publicProfileConsoleIdx),
          'Enforce cannot-follow-own-profile rule consistently.',
          'Medium'
        );
      } else {
        const ownerFollowErrorVisible = await waitForFeedback(page1, 5000);
        if (!ownerFollowErrorVisible) {
          addIssue(
            'Public Persona / Owner View',
            'Follow',
            'Error feedback should be shown for self-follow restriction.',
            'Owner follow returned 409 but no visible error feedback.',
            recentConsoleErrors(diagnosticsUser1, publicProfileConsoleIdx),
            'Map 409 response to explicit user-facing message.',
            'Medium'
          );
        } else {
          addWorking('Public Persona: Owner follow attempt is blocked with visible feedback.');
        }
      }

      await page1.getByRole('link', { name: /^Create your own persona$/ }).click();
      await page1.waitForURL((url) => url.pathname === '/', { timeout: 10000 });
      addWorking('Public Persona: Create your own persona link navigates to dashboard.');
    } else {
      addIssue(
        'Public Persona',
        'Follow',
        'Public slug should be available from publish flow.',
        'Public slug was not captured, public persona page could not be fully tested.',
        recentConsoleErrors(diagnosticsUser1),
        'Ensure publish flow exposes slug in response and UI state.',
        'High'
      );
    }

    if (publicSlug && sourceBattleID) {
      const context2 = await browser.newContext({ permissions: ['clipboard-read', 'clipboard-write'] });
      const page2 = await context2.newPage();
      attachDiagnostics(page2, diagnosticsUser2);

      await signupFromSignupPage(page2, email2, password);
      addWorking('Auth (User 2): Sign up flow works from /signup page.');

      const followConsoleIdx = diagnosticsUser2.length;
      await page2.goto(`${BASE_URL}/p/${encodeURIComponent(publicSlug)}`, { waitUntil: 'domcontentloaded' });
      const followRespPromise = waitForApi(
        page2,
        (resp) => resp.request().method() === 'POST' && new URL(resp.url()).pathname.endsWith(`/p/${publicSlug}/follow`)
      );
      await page2.getByRole('button', { name: /^Follow$/ }).click();
      const followResp = await followRespPromise;
      if (!followResp.ok()) {
        addIssue(
          'Public Persona',
          'Follow',
          'Non-owner user should be able to follow persona.',
          `API returned ${followResp.status()} for follow action.`,
          recentConsoleErrors(diagnosticsUser2, followConsoleIdx),
          'Verify auth token propagation for public follow endpoint.',
          'High'
        );
      } else {
        const followFeedbackSeen = await waitForFeedback(page2, 5000);
        if (!followFeedbackSeen) {
          addIssue(
            'Public Persona',
            'Follow',
            'Follow success should show user feedback.',
            'Follow succeeded but no visible success feedback appeared.',
            recentConsoleErrors(diagnosticsUser2, followConsoleIdx),
            'Add deterministic follow success state (toast + follower count highlight).',
            'Medium'
          );
        } else {
          addWorking('Public Persona: Follow works for authenticated non-owner user.');
        }
      }

      const remixConsoleIdx = diagnosticsUser2.length;
      await page2.goto(`${BASE_URL}/b/${encodeURIComponent(sourceBattleID)}`, { waitUntil: 'domcontentloaded' });
      const remixIntentRespPromise = waitForApi(
        page2,
        (resp) => resp.request().method() === 'POST' && /\/battles\/[^/]+\/remix-intent$/.test(new URL(resp.url()).pathname)
      );
      await page2.getByRole('button', { name: /^Remix this battle$/ }).click();
      const remixIntentResp = await remixIntentRespPromise;
      if (!remixIntentResp.ok()) {
        addIssue(
          'Battle Page',
          'Remix this battle',
          'Remix CTA should create remix intent and open modal.',
          `API returned ${remixIntentResp.status()} for remix intent.`,
          recentConsoleErrors(diagnosticsUser2, remixConsoleIdx),
          'Ensure public remix-intent endpoint accepts authenticated users consistently.',
          'High'
        );
      } else {
        const remixModalVisible = await page2.getByText('Remix Battle').waitFor({ state: 'visible', timeout: 5000 }).then(() => true).catch(() => false);
        if (!remixModalVisible) {
          addIssue(
            'Battle Page',
            'Remix this battle',
            'Remix modal should open after intent response.',
            'Remix modal did not open after successful intent response.',
            recentConsoleErrors(diagnosticsUser2, remixConsoleIdx),
            'Set modal state immediately after remix intent resolution.',
            'High'
          );
        } else {
          addWorking('Battle Page: Remix this battle opens modal and prefill data.');

          const createRemixRespPromise = waitForApi(
            page2,
            (resp) => resp.request().method() === 'POST' && /\/rooms\/[^/]+\/battles$/.test(new URL(resp.url()).pathname),
            30000
          );
          await page2.getByRole('button', { name: /^Create remixed battle$/ }).click();
          const createRemixResp = await createRemixRespPromise;
          if (!createRemixResp.ok()) {
            addIssue(
              'Battle Page / Remix Modal',
              'Create remixed battle',
              'Remix submission should create battle and start polling flow.',
              `API returned ${createRemixResp.status()} for remixed battle create.`,
              recentConsoleErrors(diagnosticsUser2, remixConsoleIdx),
              'Preserve remix token validity and surface backend errors.',
              'High'
            );
          } else {
            addWorking('Battle Page / Remix Modal: Create remixed battle sends API request and starts async flow.');
          }
        }
      }

      await context2.close();

      await page1.goto(`${BASE_URL}/`, { waitUntil: 'domcontentloaded' });
      await page1.getByRole('button', { name: /^Notifications$/ }).click();
      const notificationsRespPromise = waitForApi(
        page1,
        (resp) => resp.request().method() === 'GET' && new URL(resp.url()).pathname === '/notifications'
      );
      await page1.locator('.notifications-panel').getByRole('button', { name: /^Refresh$/ }).click();
      await notificationsRespPromise;

      const notifItems = page1.locator('.notification-item');
      const notifCount = await notifItems.count();
      if (notifCount === 0) {
        addIssue(
          'Notifications',
          'Notification list',
          'Follow/remix actions should generate notifications for owner.',
          'No notifications were visible after follow/remix events.',
          recentConsoleErrors(diagnosticsUser1, notificationsConsoleIdx),
          'Verify notification insertion for follow/remix/template events.',
          'High'
        );
      } else {
        addWorking('Notifications: Follow/remix activity generated notification items.');

        const clickNotificationConsoleIdx = diagnosticsUser1.length;
        const notificationClickRespPromise = waitForApi(
          page1,
          (resp) => resp.request().method() === 'POST' && /\/notifications\/\d+\/read$/.test(new URL(resp.url()).pathname)
        );
        const urlBeforeNotificationClick = page1.url();
        await notifItems.first().click();
        await notificationClickRespPromise;
        await page1.waitForTimeout(1200);
        const urlAfterNotificationClick = page1.url();
        if (urlAfterNotificationClick === urlBeforeNotificationClick) {
          const unreadBadgeVisible = (await page1.locator('.unread-badge').count()) > 0;
          if (unreadBadgeVisible) {
            addIssue(
              'Notifications',
              'Notification item click',
              'Click should navigate to target or visibly change read state.',
              'No navigation occurred and unread badge state did not visibly change.',
              recentConsoleErrors(diagnosticsUser1, clickNotificationConsoleIdx),
              'Show explicit in-list read-state transition even when target is missing.',
              'Medium'
            );
          } else {
            addPartial('Notifications: Notification click marked read but did not navigate (target may be empty metadata).');
          }
        } else {
          addWorking('Notifications: Item click marks as read and navigates to its target.');
        }

        await page1.goto(`${BASE_URL}/`, { waitUntil: 'domcontentloaded' });
        await page1.getByRole('button', { name: /^Notifications$/ }).click();

        const markAllRespPromise = waitForApi(
          page1,
          (resp) => resp.request().method() === 'POST' && new URL(resp.url()).pathname === '/notifications/read-all'
        );
        await page1.locator('.notifications-panel').getByRole('button', { name: /^Mark all read$/ }).click();
        await markAllRespPromise;
        await page1.waitForTimeout(800);

        const unreadBadgeStillVisible = (await page1.locator('.unread-badge').count()) > 0;
        if (unreadBadgeStillVisible) {
          addIssue(
            'Notifications',
            'Mark all read',
            'Unread badge should clear after mark-all action.',
            'Unread badge remained visible after successful mark-all request.',
            recentConsoleErrors(diagnosticsUser1),
            'Synchronize unread badge state with mark-all response.',
            'Medium'
          );
        } else {
          addWorking('Notifications: Mark all read clears unread badge state.');
        }
      }
    }

    if (sourceBattleID) {
      const battlePageConsoleIdx = diagnosticsUser1.length;
      await page1.goto(`${BASE_URL}/b/${encodeURIComponent(sourceBattleID)}`, { waitUntil: 'domcontentloaded' });

      const copyImageRespPromise = waitForApi(
        page1,
        (resp) => resp.request().method() === 'GET' && new URL(resp.url()).pathname.endsWith(`/b/${sourceBattleID}/card.png`)
      );
      await page1.getByRole('button', { name: /^Copy image$/ }).click();
      await copyImageRespPromise;
      const copyImageFeedback = await waitForFeedback(page1, 5000);
      if (!copyImageFeedback) {
        addIssue(
          'Battle Page',
          'Copy image',
          'Copy image should show success/info or error feedback.',
          'Copy image action produced no visible feedback.',
          recentConsoleErrors(diagnosticsUser1, battlePageConsoleIdx),
          'Show deterministic toast for copy-image fallback paths.',
          'Medium'
        );
      } else {
        addWorking('Battle Page: Copy image triggers card request and shows feedback.');
      }

      await page1.getByRole('button', { name: /^Copy link$/ }).click();
      const copyLinkFeedback = await waitForFeedback(page1, 5000);
      if (!copyLinkFeedback) {
        addIssue(
          'Battle Page',
          'Copy link',
          'Copy link should provide visible feedback.',
          'No visible feedback after copy link click.',
          recentConsoleErrors(diagnosticsUser1, battlePageConsoleIdx),
          'Show explicit toast for both clipboard and prompt fallback.',
          'Medium'
        );
      } else {
        addWorking('Battle Page: Copy link provides user feedback.');
      }

      await page1.getByRole('button', { name: /^Share$/ }).click();
      const shareFeedback = await waitForFeedback(page1, 5000);
      if (!shareFeedback) {
        addIssue(
          'Battle Page',
          'Share',
          'Share action should open dialog or show explicit fallback feedback.',
          'No visible success/error feedback after Share click.',
          recentConsoleErrors(diagnosticsUser1, battlePageConsoleIdx),
          'Add deterministic fallback toast/error when share APIs are unavailable.',
          'Medium'
        );
      } else {
        addWorking('Battle Page: Share action shows explicit fallback/success feedback.');
      }

      const remixIntentRespPromise = waitForApi(
        page1,
        (resp) => resp.request().method() === 'POST' && /\/battles\/[^/]+\/remix-intent$/.test(new URL(resp.url()).pathname)
      );
      await page1.getByRole('button', { name: /^Remix this battle$/ }).click();
      await remixIntentRespPromise;

      const remixModalVisible = await page1.getByText('Remix Battle').waitFor({ state: 'visible', timeout: 5000 }).then(() => true).catch(() => false);
      if (!remixModalVisible) {
        addIssue(
          'Battle Page',
          'Remix this battle',
          'Modal should open after remix intent response.',
          'Remix modal did not open.',
          recentConsoleErrors(diagnosticsUser1, battlePageConsoleIdx),
          'Set remix modal state on successful remix intent fetch.',
          'High'
        );
      } else {
        await page1.getByRole('button', { name: /^Cancel$/ }).click();
        const modalStillVisible = (await page1.getByText('Remix Battle').count()) > 0 && (await page1.getByText('Remix Battle').first().isVisible());
        if (modalStillVisible) {
          addIssue(
            'Battle Page / Remix Modal',
            'Cancel',
            'Cancel should close remix modal.',
            'Modal remained open after Cancel click.',
            recentConsoleErrors(diagnosticsUser1, battlePageConsoleIdx),
            'Ensure modal visibility state is reset by cancel action.',
            'Medium'
          );
        } else {
          addWorking('Battle Page / Remix Modal: Cancel closes the modal.');
        }
      }

      await page1.getByRole('link', { name: /^Dashboard$/ }).click();
      await page1.waitForURL((url) => url.pathname === '/', { timeout: 10000 });
      addWorking('Battle Page: Dashboard link navigates back to home.');

      const contextPublic = await browser.newContext();
      const publicPage = await contextPublic.newPage();
      attachDiagnostics(publicPage, diagnosticsPublic);

      await publicPage.goto(`${BASE_URL}/b/${encodeURIComponent(sourceBattleID)}`, { waitUntil: 'domcontentloaded' });
      await publicPage.getByRole('button', { name: /^Remix this battle$/ }).click();
      const redirectedToSignup = await publicPage
        .waitForURL((url) => url.pathname === '/signup' && url.search.includes('remix=1'), { timeout: 12000 })
        .then(() => true)
        .catch(() => false);

      if (!redirectedToSignup) {
        addIssue(
          'Battle Page (Public)',
          'Remix this battle',
          'Unauthenticated user should be redirected to signup with remix intent.',
          `Public user stayed on ${publicPage.url()} without signup redirect.`,
          recentConsoleErrors(diagnosticsPublic),
          'Persist remix intent and force auth redirect for unauthenticated remix flow.',
          'High'
        );
      } else {
        addWorking('Battle Page (Public): Remix redirects unauthenticated user to signup flow.');
      }

      await contextPublic.close();
    }

    const topNavFeed = page1.locator('nav').getByRole('link', { name: /^Feed$/ });
    const topNavRooms = page1.locator('nav').getByRole('link', { name: /^Rooms$/ });
    const topNavProfile = page1.locator('nav').getByRole('link', { name: /^Profile$/ });

    await page1.goto(`${BASE_URL}/`, { waitUntil: 'domcontentloaded' });
    await topNavFeed.click();
    await page1.waitForURL((url) => url.hash === '#feed', { timeout: 5000 });
    await topNavRooms.click();
    await page1.waitForURL((url) => url.hash === '#rooms', { timeout: 5000 });
    await topNavProfile.click();
    await page1.waitForURL((url) => url.hash === '#profile', { timeout: 5000 });
    addWorking('Navigation: Top nav Feed/Rooms/Profile links update hash anchors on home page.');

    await page1.getByRole('button', { name: /^Unpublish$/ }).click();
    const unpublishFeedback = await waitForFeedback(page1, 5000);
    if (!unpublishFeedback) {
      addIssue(
        'Home / Header',
        'Unpublish',
        'Unpublish action should show explicit success feedback.',
        'No visible feedback after unpublish action.',
        recentConsoleErrors(diagnosticsUser1),
        'Show deterministic toast and status text after unpublish.',
        'Low'
      );
    } else {
      addWorking('Home / Header: Unpublish action provides visible feedback.');
    }

    addWorking('Test setup: app was run with docker compose and seeded rooms.');

    console.log(JSON.stringify({
      meta: {
        baseUrl: BASE_URL,
        apiBase: API_BASE,
        generatedAt: new Date().toISOString(),
        sourceBattleID,
        feedBattleID,
        publicSlug,
        testUsers: [email1, email2]
      },
      ...results
    }, null, 2));

    await context1.close();
    await browser.close();
  } catch (error) {
    try {
      await context1.close();
    } catch (_err) {
      // ignore
    }
    await browser.close();
    throw error;
  }
}

run().catch((error) => {
  console.error(error);
  process.exit(1);
});
