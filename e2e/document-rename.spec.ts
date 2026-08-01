import { test, expect } from '@playwright/test';

test.describe('Document Rename UI sync', () => {
  test('should update document details panel info when renaming selected document', async ({ page }) => {
    const mockDoc = {
      id: 'doc-123',
      name: 'original-name.pdf',
      page_count: 5,
      status: 'ready',
      created_at: new Date().toISOString(),
      file_size_bytes: 1024,
    };

    let currentName = 'original-name.pdf';

    // Mock documents list endpoint
    await page.route('**/api/documents', async (route) => {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([{ ...mockDoc, name: currentName }]),
        });
      } else {
        await route.continue();
      }
    });

    // Mock get document detail endpoint
    await page.route('**/api/documents/doc-123', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ ...mockDoc, name: currentName }),
      });
    });

    // Mock rename document endpoint
    await page.route('**/api/documents/doc-123/rename', async (route) => {
      const postData = JSON.parse(route.request().postData() || '{}');
      currentName = postData.name || 'renamed-doc.pdf';
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ id: 'doc-123', name: currentName }),
      });
    });

    // Navigate to dashboard
    await page.goto('/');

    // Select document in library panel
    const docRow = page.locator('text=original-name.pdf');
    await expect(docRow).toBeVisible();
    await docRow.click();

    // Verify document details header displays original name
    const detailsHeader = page.locator('h2', { hasText: 'original-name.pdf' });
    await expect(detailsHeader).toBeVisible();

    // Click rename button on the library row
    const renameBtn = page.locator('button[aria-label="Rename original-name.pdf"]');
    await renameBtn.click();

    // Fill in new document name and press Enter / blur
    const renameInput = page.locator('input[value="original-name.pdf"]');
    await renameInput.fill('renamed-doc.pdf');
    await renameInput.press('Enter');

    // Verify Document Details panel header updates to new name
    const updatedDetailsHeader = page.locator('h2', { hasText: 'renamed-doc.pdf' });
    await expect(updatedDetailsHeader).toBeVisible();
  });
});
