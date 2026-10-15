export function bookmarkletCode(origin: string): string {
  const quotedOrigin = JSON.stringify(origin)
  return `javascript:(()=>{if(!/(^|\\.)espn\\.com$/.test(location.hostname)){alert('Open espn.com while logged in, then click this bookmark.');return}const c={};document.cookie.split('; ').forEach(p=>{const i=p.indexOf('=');c[p.slice(0,i)]=p.slice(i+1)});if(!c.espn_s2||!c.SWID){alert('Log in to ESPN first.');return}const e=v=>encodeURIComponent(decodeURIComponent(v));window.open(${quotedOrigin}+'/connect#s2='+e(c.espn_s2)+'&swid='+e(c.SWID))})()`
}
