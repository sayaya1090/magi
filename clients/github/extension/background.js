chrome.runtime.onInstalled.addListener(()=>{chrome.sidePanel.setPanelBehavior({openPanelOnActionClick:true}).catch(()=>console.error('Magi 사이드 패널을 설정하지 못했습니다. 확장을 새로고침해 주세요.'));});
chrome.storage.session.setAccessLevel({accessLevel:'TRUSTED_CONTEXTS'}).catch(()=>console.error('Magi 토큰 저장소 접근 제한을 설정하지 못했습니다. 확장을 새로고침해 주세요.'));
