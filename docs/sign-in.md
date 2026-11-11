---
title: Sign-in
summary: How the panel signs in with security keys and passkeys, adding backup keys, and recovering when every key is lost.
---

The panel signs in with security keys (like a YubiKey) and passkeys, with no password. The API under `/v1` uses [keys](keys.md) instead and never needs the panel's sign-in.

## First key

When no key exists, Mainspring prints a one-time setup code in its log when it starts:

```text
mainspring setup code: YFZ8-G9FA — open the panel to add your first security key
```

Open the panel over HTTPS or on `localhost`, enter the code and add a key. Browsers only offer security keys and passkeys there, so a plain `http://` address on your network won't work; use the [tailnet address](tailnet.md) or another HTTPS address.

## More keys

Add keys under **Settings → Sign-in**. Add a second one as a backup and keep it somewhere safe; Mainspring won't let you remove your last key.

Security keys are added without a passkey, so they don't use one of your YubiKey's passkey slots. Passkeys are stored by your browser or phone.

A key works on the address it was added on. If you open the panel at a second address, add a key there too.

## Behind Umbrel or another sign-in proxy

With `MAINSPRING_AUTH=proxy`, the proxy in front of Mainspring signs you in, and the panel doesn't ask for a key. On Umbrel this is the default. You can still add keys and turn on **Also require a key** under **Settings → Sign-in**. The [tailnet address](tailnet.md) always asks for a key, because it skips the proxy.

## Lost every key

Run this on the machine Mainspring runs on:

```sh
mainspring auth reset
```

It removes every key, signs out every panel session and prints a new setup code. On Docker, run it inside the container (`docker exec mainspring /mainspring auth reset`). `mainspring auth status` lists the keys without changing anything.
