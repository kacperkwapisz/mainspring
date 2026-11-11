---
title: Tailnet address
summary: Give Mainspring its own HTTPS address on your Tailscale network, set up from Settings.
---

Mainspring can join your [Tailscale](https://tailscale.com) network as its own device, with an address like `https://mainspring.your-tailnet.ts.net` that every device on your tailnet can reach. Tools and the panel both work there, with a real HTTPS certificate.

## Set it up

1. Open **Settings → Tailnet address** and choose **Add to tailnet**.
2. Name the device. `mainspring` is the default; the name becomes the first part of the address.
3. Sign in to Tailscale: scan the QR code with your phone, open the link on this computer, or paste an auth key from Tailscale's admin console for a setup without a browser.
4. Mainspring joins, then gets an HTTPS certificate through Tailscale. This can take a minute; you can close the dialog meanwhile.

The device's keys live in Mainspring's data folder (`tailscale/` next to the database), so it stays on your tailnet across restarts and updates.

## HTTPS

HTTPS needs **Enable HTTPS** under **DNS** in Tailscale's admin console. Until it's on, Mainspring joins without a certificate: tools can use `http://…/v1`, but security keys don't work there, because browsers only allow them over HTTPS. Turn it on, then choose **Check again**.

Plain `http://` requests to the panel move to HTTPS once it works. API calls on `http://` keep working, so tools aren't redirected mid-request.

## Signing in there

The tailnet address doesn't go through Umbrel's login, or any other sign-in proxy in front of Mainspring. The panel there always asks for one of Mainspring's own security keys or passkeys, and security keys belong to the address they were added on.

The first time, choose **Open it** on the Address row in Settings. While no key exists for the tailnet address, it opens there with a one-time setup code, so you can add one straight away.

## Removing it

**Remove from tailnet** signs the device out of Tailscale and deletes its keys. The address stops working. You can add it again any time.
