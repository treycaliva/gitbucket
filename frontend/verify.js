import { chromium } from 'playwright';

(async () => {
  const browser = await chromium.launch();
  const context = await browser.newContext();
  const page = await context.newPage();

  // Create a mock route for the pull request data
  await page.route('**/api/repos/user/repo/pulls/1', async route => {
    const json = {
      title: "Test PR",
      status: "open",
      sourceBranch: "feature",
      targetBranch: "main",
      author: "user"
    };
    await route.fulfill({ json });
  });

  await page.route('**/api/repos/user/repo/pulls/1/diff', async route => {
    // Generate a very large diff
    let rawDiff = "";
    for (let i = 0; i < 50; i++) {
      rawDiff += `diff --git a/folder1/folder2/file${i}.js b/folder1/folder2/file${i}.js
--- a/folder1/folder2/file${i}.js
+++ b/folder1/folder2/file${i}.js
@@ -1,3 +1,3 @@
-old code
+new code
`;
    }
    await route.fulfill({ body: rawDiff });
  });

  await page.route('**/api/repos/user/repo/pulls/1/reviews', async route => {
    await route.fulfill({ json: [] });
  });

  await page.route('**/api/repos/user/repo/pulls/1/commits', async route => {
    await route.fulfill({ json: [] });
  });

  await page.route('**/api/repos/user/repo/branch-protection', async route => {
    await route.fulfill({ json: [] });
  });

  await page.route('**/api/repos/user/repo/collaborators', async route => {
    await route.fulfill({ json: [] });
  });

  await page.route('**/api/repos/user/repo', async route => {
    await route.fulfill({ json: { owner: "user", name: "repo", defaultBranch: "main" } });
  });

  await page.route('**/api/user/me', async route => {
    await route.fulfill({ json: { username: "user" } });
  });

  // Start tracing to capture a screenshot
  await context.tracing.start({ screenshots: true, snapshots: true });

  await page.goto('http://localhost:3000/r/user/repo/pulls/1');

  // Wait for the page to load
  await page.waitForTimeout(5000);

  // Take a screenshot
  await page.screenshot({ path: 'verify.png', fullPage: true });

  await context.tracing.stop({ path: 'trace.zip' });
  await browser.close();
  console.log("Done");
})();
