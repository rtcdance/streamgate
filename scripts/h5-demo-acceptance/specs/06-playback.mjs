// Spec 06: real browser playback.
//
// The other specs stop at "the player section is visible". That is not playback:
// a 404 CONTENT_NOT_FOUND, a 200 manifest with no segments, or a stalled player
// all leave the DOM looking fine. This spec drives hls.js against a real,
// transcoded stream and asserts the media element actually advances:
// loadedmetadata, a decoded frame (videoWidth > 0), currentTime growing by at
// least 3 seconds, and no fatal hls.js error.
//
// The stream is described by a JSON file written by
// scripts/fullchain-acceptance.sh (SG_PLAYBACK_FILE). If it is missing the spec
// FAILS rather than skipping: a key test that did not run is not a pass.

import { readFileSync } from 'node:fs';
import { check } from '../lib/common.mjs';
import { H5_BASE } from '../lib/urls.mjs';

const DESCRIPTOR = process.env.SG_PLAYBACK_FILE || '/tmp/sg-playback.json';
const PLAY_SECONDS = 3;
const OBSERVE_MS = 15000;

export default async function spec06Playback({ page, snap }) {
    const checks = [];

    let desc;
    try {
        desc = JSON.parse(readFileSync(DESCRIPTOR, 'utf8'));
    } catch (e) {
        checks.push(check('playback-descriptor', false, `cannot read ${DESCRIPTOR}: ${e.message}`));
        return { passed: false, checks };
    }
    if (!desc.entryUrl || !desc.jwt) {
        checks.push(check('playback-descriptor', false, `entryUrl/jwt missing: ${JSON.stringify(desc)}`));
        return { passed: false, checks };
    }
    checks.push(check('playback-descriptor', true, `content=${desc.contentId}`));

    await page.goto(`${H5_BASE}/`, { waitUntil: 'domcontentloaded' });
    await page.addScriptTag({ url: `${H5_BASE}/vendor/hls.min.js` });
    const hlsAvailable = await page.evaluate(() => typeof window.Hls !== 'undefined');
    checks.push(check('hlsjs-available', hlsAvailable, 'the demo ships hls.js'));
    if (!hlsAvailable) return { passed: false, checks };

    const result = await page.evaluate(async ({ url, jwt, playSeconds, observeMs }) => {
        const out = {
            events: [],
            fatal: null,
            playError: null,
            currentTime: 0,
            videoWidth: 0,
            videoHeight: 0,
            duration: null,
            readyState: 0,
            stalls: 0,
            firstFrameAt: null,
        };

        const video = document.createElement('video');
        video.muted = true; // satisfies autoplay policy without a gesture
        video.playsInline = true;
        video.id = 'sg-playback-probe';
        video.style.width = '320px';
        document.body.appendChild(video);

        const hls = new window.Hls({
            enableWorker: false,
            // The entry manifest is behind JWT auth; segments carry their own
            // playback_token, so only the playlist requests need the header.
            xhrSetup: (xhr, u) => {
                if (u.includes('manifest.m3u8')) xhr.setRequestHeader('Authorization', `Bearer ${jwt}`);
            },
        });

        let lastTime = 0;
        video.addEventListener('loadedmetadata', () => out.events.push('loadedmetadata'));
        video.addEventListener('playing', () => out.events.push('playing'));
        video.addEventListener('waiting', () => { out.stalls += 1; out.events.push('waiting'); });
        video.addEventListener('timeupdate', () => {
            if (out.firstFrameAt === null && video.currentTime > 0) out.firstFrameAt = video.currentTime;
            lastTime = video.currentTime;
        });

        hls.on(window.Hls.Events.MANIFEST_PARSED, () => out.events.push('MANIFEST_PARSED'));
        hls.on(window.Hls.Events.LEVEL_LOADED, () => out.events.push('LEVEL_LOADED'));
        hls.on(window.Hls.Events.FRAG_LOADED, () => out.events.push('FRAG_LOADED'));
        hls.on(window.Hls.Events.ERROR, (_evt, data) => {
            out.events.push(`ERROR:${data.details}`);
            if (data.fatal) out.fatal = `${data.type}/${data.details}`;
        });

        const finished = new Promise((resolve) => {
            const started = Date.now();
            const tick = () => {
                // Stop as soon as we have observed real progress, or time out.
                if (video.currentTime >= playSeconds + 0.5 || Date.now() - started > observeMs) return resolve();
                setTimeout(tick, 250);
            };
            tick();
        });

        hls.loadSource(url);
        hls.attachMedia(video);
        try {
            await video.play();
        } catch (e) {
            out.playError = String(e);
        }
        await finished;

        out.currentTime = video.currentTime;
        out.videoWidth = video.videoWidth;
        out.videoHeight = video.videoHeight;
        out.duration = Number.isFinite(video.duration) ? video.duration : null;
        out.readyState = video.readyState;
        try { hls.destroy(); } catch { /* already torn down */ }
        video.remove();
        return out;
    }, { url: desc.entryUrl, jwt: desc.jwt, playSeconds: PLAY_SECONDS, observeMs: OBSERVE_MS });

    await snap('06-playback');

    const sawLoadedMetadata = result.events.includes('loadedmetadata');
    checks.push(check('playback-loadedmetadata', sawLoadedMetadata,
        `events=${result.events.slice(0, 12).join(',')}`));

    const decodedFrame = result.videoWidth > 0 && result.videoHeight > 0;
    checks.push(check('playback-decoded-frames', decodedFrame,
        `videoWidth=${result.videoWidth} videoHeight=${result.videoHeight} readyState=${result.readyState}`));

    checks.push(check('playback-currenttime-advances', result.currentTime >= PLAY_SECONDS,
        `currentTime=${result.currentTime.toFixed(2)}s (need >= ${PLAY_SECONDS}s), firstFrameAt=${result.firstFrameAt}`));

    checks.push(check('playback-no-fatal-error', result.fatal === null && result.playError === null,
        `fatal=${result.fatal} playError=${result.playError}`));

    return { passed: checks.every((c) => c.ok), checks };
}
