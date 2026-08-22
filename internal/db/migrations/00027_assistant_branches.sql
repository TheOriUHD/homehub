-- +goose Up
-- A conversation is a tree, not a list: editing a question or retrying an
-- answer forks a new branch off the same parent, and the reader walks one
-- path through it. Existing rows are back-filled into a single chain so old
-- conversations keep reading exactly as they did.
alter table assistant_messages
    add column parent_id bigint references assistant_messages (id) on delete cascade;

-- +goose StatementBegin
do $$
declare
    prev bigint;
    cur  record;
    scope record;
begin
    for scope in
        select distinct user_id, channel, conversation from assistant_messages
    loop
        prev := null;
        for cur in
            select id from assistant_messages
            where user_id = scope.user_id and channel = scope.channel
              and conversation = scope.conversation
            order by id asc
        loop
            update assistant_messages set parent_id = prev where id = cur.id;
            prev := cur.id;
        end loop;
    end loop;
end $$;
-- +goose StatementEnd

create index assistant_messages_parent_idx on assistant_messages (parent_id);

-- +goose Down
drop index if exists assistant_messages_parent_idx;
alter table assistant_messages drop column if exists parent_id;
