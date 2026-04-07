import asyncio
import os
from playwright.async_api import async_playwright

async def test_leave_logic():
    async with async_playwright() as p:
        browser = await p.chromium.launch(headless=True)
        
        # Context A (Player A)
        contextA = await browser.new_context()
        pageA = await contextA.new_page()
        
        # Context B (Player B)
        contextB = await browser.new_context()
        pageB = await contextB.new_page()

        BASE_URL = "http://localhost:31744" 
        # Using localhost since we are local. Assumes server is running on 31744 (from src/main.c)
        
        print("Navigating to app...")
        await pageA.goto(BASE_URL)
        await pageB.goto(BASE_URL)

        # Handle alerts
        async def handle_dialog(dialog):
            print(f"Alert: {dialog.message}")
            await dialog.accept()
            
        pageA.on("dialog", handle_dialog)
        pageB.on("dialog", handle_dialog)

        # --- Join Room 1 ---
        print("Player A joining Room 5...")
        await pageA.fill('#room-input', '5')
        await pageA.click("text=Join Othello")
        await pageA.wait_for_selector('#game-area:not(.hidden)')
        print("Player A joined.")

        print("Player B joining Room 5...")
        await pageB.fill('#room-input', '5')
        await pageB.click("text=Join Othello")
        await pageB.wait_for_selector('#game-area:not(.hidden)')
        print("Player B joined.")
        
        # Wait for sync
        await asyncio.sleep(2)
        
        # Verify both see "Active" (or at least game area)
        # Check turn indicator text to ensure game started
        textA = await pageA.inner_text('#turn-indicator')
        textB = await pageB.inner_text('#turn-indicator')
        print(f"State A: {textA}")
        print(f"State B: {textB}")
        
        if "Waiting" in textA or "Waiting" in textB:
             print("Game didn't start properly?")
        
        # --- Player A Leaves ---
        print("Player A clicking Leave...")
        await pageA.click('text=Leave')
        
        # Verify Player A is in Lobby
        await pageA.wait_for_selector('#lobby:not(.hidden)')
        print("Player A is in Lobby.")

        # --- Verify Player B reaction ---
        print("Waiting for Player B to detect leave...")
        # Player B should get an alert "Opponent has left..." and be forced to lobby
        # The alert handler above prints the message.
        
        try:
            await pageB.wait_for_selector('#lobby:not(.hidden)', timeout=5000)
            print("Player B is in Lobby. Success!")
        except:
            print("Player B did NOT return to lobby in time.")
            # Debug info
            visible = await pageB.is_visible('#game-area')
            print(f"Player B Game Area Visible: {visible}")

        await browser.close()

if __name__ == "__main__":
    asyncio.run(test_leave_logic())
