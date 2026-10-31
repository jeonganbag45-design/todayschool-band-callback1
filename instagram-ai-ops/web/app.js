const KEY='igops_account_blobs_v1',SEL='igops_selected_account_v1';
let accounts=[],blobs=[],selected='',status={configured:false,redirect_uri:''};
const $=id=>document.getElementById(id);
const fmt=n=>new Intl.NumberFormat('ko-KR').format(n||0);
const esc=v=>String(v||'').replace(/[&<>"']/g,m=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[m]));
function notice(msg,good=false){const el=$('notice');el.textContent=msg;el.style.display=msg?'block':'none';el.classList.toggle('good',good)}
function getBlobs(){try{return JSON.parse(localStorage.getItem(KEY)||'[]')}catch{return[]}}
function saveBlobs(v){localStorage.setItem(KEY,JSON.stringify(v))}
async function api(url,opt={}){const r=await fetch(url,{headers:{'Content-Type':'application/json'},...opt});let d={};try{d=await r.json()}catch{}if(!r.ok)throw new Error(d.error||'요청에 실패했습니다.');return d}
async function loadStatus(){status=await api('/api/status');renderStatus()}
function renderStatus(){
  $('redirectUri').textContent=status.redirect_uri||location.origin+'/auth/instagram/callback';
  $('metaDot').classList.toggle('ok',!!status.configured);
  $('metaBadge').classList.toggle('ok',!!status.configured);
  $('metaText').textContent=status.configured?'연결 준비 완료':'서버 설정 필요';
  $('setupText').textContent=status.configured?'Meta Instagram 앱 설정이 서버에 준비되어 있습니다.':'아직 Meta Instagram 앱 설정이 서버에 없습니다.';
  $('setupHelp').textContent=status.configured?'이제 아래 공식 로그인 버튼을 사용할 수 있습니다.':'Render의 Environment에 META_INSTAGRAM_APP_ID와 META_INSTAGRAM_APP_SECRET을 직접 저장해야 합니다. 이 사이트 화면에는 Secret이나 Access Token을 입력하지 마세요.';
  $('loginHelp').textContent=status.configured?'Meta/Instagram 공식 인증 페이지로 이동합니다.':'서버 설정이 끝난 뒤 사용할 수 있습니다.';
  $('instagramLoginBtn').disabled=!status.configured;
}
async function loadAccounts(){
  blobs=getBlobs();
  if(blobs.length){const d=await api('/api/accounts/resolve',{method:'POST',body:JSON.stringify({blobs})});accounts=d.accounts||[];blobs=d.blobs||[];saveBlobs(blobs)}else accounts=[];
  selected=localStorage.getItem(SEL)||accounts[0]?.id||'';
  if(selected&&!accounts.find(a=>a.id===selected))selected=accounts[0]?.id||'';
  render();
}
function render(){
  const sel=$('accountSelect');
  sel.innerHTML=accounts.length?accounts.map(a=>`<option value="${esc(a.id)}" ${a.id===selected?'selected':''}>@${esc(a.username)}</option>`).join(''):'<option value="">연결된 계정 없음</option>';
  const a=accounts.find(x=>x.id===selected);
  if(!a){$('sideName').textContent='계정 미연결';$('sideFollowers').textContent='Instagram 연결 필요';$('sideAvatar').removeAttribute('src');$('noAccount').classList.remove('hidden');$('accountHome').classList.add('hidden');return}
  $('noAccount').classList.add('hidden');$('accountHome').classList.remove('hidden');$('sideName').textContent='@'+a.username;$('sideFollowers').textContent=fmt(a.followers_count)+' 팔로워';
  if(a.profile_picture_url){$('sideAvatar').src=a.profile_picture_url;$('profileAvatar').src=a.profile_picture_url}else{$('sideAvatar').removeAttribute('src');$('profileAvatar').removeAttribute('src')}
  $('profileName').textContent=a.name||'@'+a.username;$('profileUser').textContent='@'+a.username;$('accountType').textContent=a.account_type||'PROFESSIONAL';$('followers').textContent=fmt(a.followers_count);$('mediaCount').textContent=fmt(a.media_count);$('recentCount').textContent=(a.recent_media||[]).length;
  $('lastSync').textContent='최근 동기화: '+new Date(a.last_synced_at).toLocaleString('ko-KR');$('tokenExpiry').textContent='연결 토큰 만료: '+new Date(a.token_expires_at).toLocaleDateString('ko-KR');
  $('postsGrid').innerHTML=(a.recent_media||[]).map(p=>`<article class="post"><a href="${esc(p.permalink||'#')}" target="_blank" rel="noopener"><img src="${esc(p.thumbnail_url||p.media_url||'')}" alt=""></a><div class="post-body"><div class="caption">${esc(p.caption||'캡션 없음')}</div><div class="post-meta"><span>좋아요 ${fmt(p.like_count)}</span><span>댓글 ${fmt(p.comments_count)}</span></div></div></article>`).join('')||'<div class="placeholder">최근 게시물을 불러오지 못했습니다.</div>';
}
function openConnect(){$('connectModal').classList.remove('hidden');loadStatus().catch(e=>notice(e.message))}
function closeConnect(){$('connectModal').classList.add('hidden')}
async function handleCallback(){
  const q=new URLSearchParams(location.search),blob=q.get('connected_blob'),id=q.get('connected_id'),err=q.get('connect_error');
  if(err){notice('Instagram 연결 오류: '+err);openConnect();history.replaceState({},'',location.pathname);return}
  if(blob){const current=getBlobs();current.push(blob);saveBlobs([...new Set(current)]);if(id)localStorage.setItem(SEL,id);history.replaceState({},'',location.pathname);await loadAccounts();notice('Instagram 계정을 연결했습니다.',true)}
}
$('accountSelect').addEventListener('change',e=>{selected=e.target.value;localStorage.setItem(SEL,selected);render()});
$('syncBtn').addEventListener('click',async()=>{const i=accounts.findIndex(a=>a.id===selected);if(i<0){notice('먼저 Instagram 계정을 연결하세요.');openConnect();return}const btn=$('syncBtn'),old=btn.textContent;btn.textContent='동기화 중...';btn.disabled=true;try{const d=await api('/api/accounts/sync',{method:'POST',body:JSON.stringify({blob:blobs[i]})});blobs[i]=d.blob;accounts[i]=d.account;saveBlobs(blobs);render();notice('Instagram 계정 정보를 새로 불러왔습니다.',true)}catch(e){notice(e.message)}finally{btn.textContent=old;btn.disabled=false}});
['connectBtn','mobileConnect','emptyConnect'].forEach(id=>$(id).addEventListener('click',openConnect));
$('closeModal').addEventListener('click',closeConnect);$('connectModal').addEventListener('click',e=>{if(e.target===$('connectModal'))closeConnect()});
$('instagramLoginBtn').addEventListener('click',()=>{if(!status.configured){notice('먼저 Render 서버에 Meta Instagram 앱 설정을 저장해야 합니다.');return}location.href='/auth/instagram/start'});
document.querySelectorAll('.nav button').forEach(b=>b.addEventListener('click',()=>{document.querySelectorAll('.nav button').forEach(x=>x.classList.remove('active'));b.classList.add('active');const v=b.dataset.view;if(v==='home'){$('homeView').classList.remove('hidden');$('placeholderView').classList.add('hidden');$('pageTitle').textContent='계정 홈';$('pageDesc').textContent='선택한 Instagram 계정 기준으로 관리합니다.';return}const map={create:['게시물 만들기','최신 트렌드와 계정 성과를 반영한 제작 엔진을 다음 단계에서 연결합니다.'],posts:['내 게시물','현재는 홈에서 실제 최근 게시물을 확인할 수 있습니다.'],analytics:['성과 분석','Instagram Insights 수집 기능을 다음 단계에서 연결합니다.'],comments:['댓글 분석','댓글 수집·논란·오류 지적 분석 기능을 다음 단계에서 연결합니다.'],learning:['AI 학습','계정별 성과를 쌓아 Account Brain이 학습하도록 만들 예정입니다.'],settings:['계정 설정','Instagram 연결과 계정별 설정을 관리합니다.']};$('homeView').classList.add('hidden');$('placeholderView').classList.remove('hidden');$('pageTitle').textContent=map[v][0];$('pageDesc').textContent='선택한 계정 전용 화면';if(v==='settings'){$('placeholderView').innerHTML='<h2>계정 설정</h2><p>민감정보는 이 사이트에 입력하지 않습니다. 공식 OAuth 연결만 사용합니다.</p><button class="btn primary" id="settingsConnect">Instagram 연결 상태 보기</button>';$('settingsConnect').addEventListener('click',openConnect)}else $('placeholderView').innerHTML=`<h2>${map[v][0]}</h2><p>${map[v][1]}</p>`}));
(async()=>{try{await Promise.all([loadStatus(),loadAccounts()]);await handleCallback()}catch(e){notice(e.message)}})();
