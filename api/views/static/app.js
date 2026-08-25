// Progressive enhancement for delete controls; ordinary forms remain usable.
document.addEventListener("click",async(e)=>{const el=e.target.closest("[hx-delete]");if(!el)return;e.preventDefault();if(el.dataset.confirm&&!confirm(el.dataset.confirm))return;const r=await fetch(el.getAttribute("hx-delete"),{method:"DELETE",credentials:"same-origin"});if(r.ok)location.reload()});
