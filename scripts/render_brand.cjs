// Render the project-owned SVG; PLAYWRIGHT_MODULE may point at a local playwright-core installation.
const fs = require('fs');
const path = require('path');
const {chromium} = require(process.env.PLAYWRIGHT_MODULE || 'playwright-core');
(async()=>{
  const root=path.resolve(__dirname,'..');
  const browser=await chromium.launch({executablePath:process.env.CHROME_EXECUTABLE || undefined, headless:true});
  try {
    const page=await browser.newPage({viewport:{width:1024,height:1024},deviceScaleFactor:1});
    await page.setContent('<style>html,body{margin:0;width:1024px;height:1024px;overflow:hidden}</style>'+fs.readFileSync(path.join(root,'branding/icon.svg'),'utf8'));
    await page.screenshot({path:path.join(root,'branding/icon-1024.png')});
  } finally {await browser.close();}
})();
