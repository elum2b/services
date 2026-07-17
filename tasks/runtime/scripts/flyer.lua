function flyer_base_url(event)
    event.config.settings = event.config.settings or {}
    return event.config.settings.base_url or "https://api.flyerhubs.com"
end

function flyer_first_non_empty(...)
    for index = 1, select("#", ...) do
        local value = select(index, ...)
        if value ~= nil and tostring(value) ~= "" then
            return tostring(value)
        end
    end
    return ""
end

function flyer_partner_number(value)
    local number = tonumber(value)
    if number ~= nil then
        return number
    end
    return value
end

function flyer_button_text(external_type)
    if external_type == "subscribe channel" or external_type == "channel" or external_type == "subscribe" then
        return "Подписаться"
    end
    return "Перейти"
end

function list(event)
    event.variables = event.variables or {}
    event.config.settings = event.config.settings or {}
    local body = {
        key = event.config.secret,
        user_id = flyer_partner_number(event.identity.platform_user_id),
        language_code = event.locale,
        limit = event.limit
    }
    if body.limit == nil or body.limit <= 0 then
        body.limit = 5
    end
    local path = "/get_tasks"
    if event.config.platform == "max" then
        path = "/max/get_tasks"
        body.user_locale = event.locale
        if event.variables.chat_id ~= nil and event.variables.chat_id ~= "" then
            body.chat_id = flyer_partner_number(event.variables.chat_id)
        end
    end
    local response = http.request({
        method = "POST",
        url = flyer_base_url(event) .. path,
        headers = {
            ["Content-Type"] = "application/json"
        },
        body = json.encode(body)
    })
    if response.status >= 400 then
        return {
            ok = false,
            error = "partner_http_error",
            status = response.status,
            body = response.body
        }
    end
    local data = json.decode(response.body)
    local items = data.tasks or data.result or data.data or {}
    local tasks = {}
    for _, item in ipairs(items) do
        if item.signature ~= nil and item.signature ~= "" then
            local external_type = flyer_first_non_empty(item.task_type, item.type, "task")
            local link = flyer_first_non_empty(item.link, item.url)
            local title = flyer_first_non_empty(item.title, item.name)
            table.insert(tasks, {
                external_id = tostring(item.signature),
                external_type = external_type,
                public_payload = {
                    signature = item.signature,
                    link = link,
                    title = title,
                    button_text = flyer_first_non_empty(item.button_text, flyer_button_text(external_type)),
                    flyer_type = external_type
                },
                private_payload = {
                    signature = item.signature
                }
            })
        end
    end
    return {
        ok = true,
        tasks = tasks
    }
end

function callback(event)
    local request = event.request or {}
    local body = request.body or {}
    local status = flyer_first_non_empty(body.status, body.event, body.action, body.type)
    local action = "complete"
    if status == "unsubscribe" or status == "unsubscribed" or status == "revoked" or status == "cancelled" or status == "canceled" then
        action = "revoked"
    end
    return {
        ok = true,
        action = action,
        status = status ~= "" and status or action,
        issue_id = body.issue_id,
        issue_ref = body.issue_ref or body.task_ref,
        external_click_id = body.external_click_id or body.click_id,
        external_id = flyer_first_non_empty(body.external_id, body.signature, body.task_id, body.offer_id),
        platform_user_id = flyer_first_non_empty(body.platform_user_id, body.user_id, body.tg_user_id, body.chat_id),
        payload = {
            provider = "flyer",
            status = status,
            event = body.event,
            signature = body.signature,
            task_id = body.task_id,
            offer_id = body.offer_id,
            user_id = body.user_id
        }
    }
end

function check(event)
    event.config.settings = event.config.settings or {}
    local private = event.issue.private_payload or {}
    local path = "/check_task"
    if event.config.platform == "max" then
        path = "/max/check_task"
    end
    local response = http.request({
        method = "POST",
        url = flyer_base_url(event) .. path,
        headers = {
            ["Content-Type"] = "application/json"
        },
        body = json.encode({
            key = event.config.secret,
            signature = private.signature
        })
    })
    if response.status >= 400 then
        return {
            ok = false,
            error = "partner_http_error",
            status = response.status,
            body = response.body
        }
    end
    local data = json.decode(response.body)
    local completed = false
    if data.completed ~= nil then
        completed = data.completed == true
    elseif data.skip ~= nil then
        completed = data.skip == true
    elseif data.status == "completed" or data.status == "ok" or data.status == "done" then
        completed = true
    end
    local status = data.status
    if status == nil or status == "" then
        if completed then
            status = "completed"
        else
            status = "not_completed"
        end
    end
    if data.error ~= nil and data.error ~= "" then
        status = data.error
    end
    return {
        ok = true,
        completed = completed,
        status = status,
        payload = {
            provider = "flyer",
            status = status,
            completed = completed
        }
    }
end
