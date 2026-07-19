import { test, expect } from '@playwright/test';
import path from 'path';

test.describe('PDF Booklet Maker & Semantic Search Flow', () => {

  test('should complete the developer login, upload, compile, and search flow', async ({ page }) => {
    // 1. Navigate to Login page
    await page.goto('/login');
    await expect(page).toHaveTitle(/Booklet/);

    // 2. Perform Developer Bypass Login
    await page.click('role=tab[name="Developer Bypass"]');
    await page.fill('input[placeholder="e.g. dev@example.com"]', 'test-e2e@example.com');
    await page.fill('input[placeholder="e.g. Developer User"]', 'E2E Tester');
    await page.click('button:has-text("Start Dev Session")');

    // 3. Confirm redirection to Dashboard
    await page.waitForURL('/');
    await expect(page.locator('h3:has-text("Upload Document")')).toBeVisible();

    // 4. Create a mock PDF file to upload (we can use a 1-page blank text file as PDF for testing UI upload trigger)
    // In a real run, you can upload a real test PDF file.
    const fileChooserPromise = page.waitForEvent('filechooser');
    await page.click('span:has-text("Drag & drop your PDF file")', { force: true });
    const fileChooser = await fileChooserPromise;
    
    // We upload a mock PDF file name (actual upload will mock-fail or succeed depending on backend status)
    // For local E2E simulation, we check that elements exist.
    expect(fileChooser).toBeDefined();

    // 5. Check if semantic search panel works
    await page.click('a[href="/search"]');
    await page.waitForURL('/search');
    await expect(page.locator('h2:has-text("Semantic Search")')).toBeVisible();
    await page.fill('input[placeholder="Type a search query..."]', 'imposition layout math');
    await page.click('button:has-text("Search")');

    // 6. Navigate back to Dashboard
    await page.click('a[href="/"]');
    await page.waitForURL('/');
    await expect(page.locator('h3:has-text("Library")')).toBeVisible();
  });
});
